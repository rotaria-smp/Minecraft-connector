package awiant.connector;

import com.google.gson.Gson;
import com.google.gson.JsonObject;
import net.minecraft.network.chat.Component;
import net.minecraft.server.MinecraftServer;
import net.minecraftforge.server.ServerLifecycleHooks;

import java.io.*;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;

public class DiscordBridge {
    private ServerSocket serverSocket;
    private volatile Socket clientSocket;
    // Only used for broadcasting EVT JSON lines to the *current* client
    private volatile OutputStream eventOut;
    private final int port;
    private final Gson gson = new Gson();
    private final java.util.concurrent.BlockingQueue<String> outbox =
            new java.util.concurrent.LinkedBlockingQueue<>(10_000); // tune size as needed
    private volatile Thread writerThread;
    private volatile OutputStream writerOut; // writer's private handle (don't use elsewhere)


    public DiscordBridge(int port) {
        this.port = port;
        startServer();
    }

    private void startServer() {
        new Thread(this::acceptLoop, "discord-bridge-accept").start();
    }

    private void acceptLoop() {
        try {
            serverSocket = new ServerSocket(port);
            Connector.LOGGER.info("Discord bridge (NDJSON) listening on port {}", port);
            while (!serverSocket.isClosed()) {
                Socket s = serverSocket.accept();
                s.setTcpNoDelay(true);
                replaceClient(s);
                Connector.LOGGER.info("accepted {}", s);
                new Thread(() -> handleClient(s), "discord-bridge-client").start();
            }
        } catch (IOException e) {
            Connector.LOGGER.error("Error in Discord bridge", e);
        }
    }

    private synchronized void startWriterThread(Socket s) throws IOException {
        stopWriterThread(); // stop any previous writer first

        // Prepare the new output stream and set socket options
        s.setTcpNoDelay(true);
        s.setKeepAlive(true);
        try {
            s.setSendBufferSize(256 * 1024); // optional, helps with bursts
        } catch (Exception ignore) {}

        writerOut = s.getOutputStream();

        writerThread = new Thread(() -> {
            try {
                final OutputStream o = writerOut; // capture for this generation
                for (;;) {
                    // Blocks until there's a line to send; each line already ends with '\n'
                    String line = outbox.take();
                    byte[] bytes = line.getBytes(StandardCharsets.UTF_8);

                    // Keep each write+flush atomic to avoid interleaving with future changes
                    synchronized (this) {
                        // If a new client replaced us, bail out quietly
                        if (o != writerOut) break;

                        o.write(bytes);
                        o.flush();
                    }
                }
            } catch (InterruptedException ie) {
                // normal shutdown path
                Thread.currentThread().interrupt();
            } catch (IOException ioe) {
                Connector.LOGGER.warn("Writer thread IO ended: {}", ioe.toString());
                // Do NOT call closeClient() here; the accept loop manages lifecycle.
            } finally {
                // Best effort: let this generation's stream go
                synchronized (this) {
                    if (writerOut != null) {
                        try { writerOut.flush(); } catch (IOException ignore) {}
                    }
                }
            }
        }, "discord-bridge-writer");

        writerThread.setDaemon(true);
        writerThread.start();
    }

    private synchronized void stopWriterThread() {
        Thread wt = writerThread;
        writerThread = null;
        // Detach writerOut so any running writer exits if it wakes up
        writerOut = null;

        if (wt != null) {
            wt.interrupt();
            // Don't join() here to avoid blocking server threads
        }
        outbox.clear(); // drop stale messages for previous client
    }

    private synchronized void replaceClient(Socket s) throws IOException {
        closeClient();           // shuts down old client + writer
        clientSocket = s;
        eventOut = null;         // deprecated: writerOut is authoritative
        startWriterThread(s);
    }

    private synchronized void closeClient() {
        stopWriterThread();
        try { if (clientSocket != null) clientSocket.close(); } catch (IOException ignore) {}
        clientSocket = null;
        eventOut = null;
    }

    private void handleClient(Socket s) {
        try (InputStream rin = s.getInputStream();
             OutputStream rout = s.getOutputStream();
             BufferedReader hin = new BufferedReader(new InputStreamReader(rin, StandardCharsets.UTF_8))) {

            for (;;) {
                String line = hin.readLine();
                if (line == null) break;
                line = line.trim();
                if (line.isEmpty()) continue;

                Connector.LOGGER.debug("recv json: {}", line);
                JsonObject m = gson.fromJson(line, JsonObject.class);
                if (m == null || !m.has("type")) {
                    Connector.LOGGER.warn("bad json frame: {}", line);
                    continue;
                }
                String type = m.get("type").getAsString();
                switch (type) {
                    case "PING":
                        enqueueJson(json("type","PONG"));
                        break;
                    case "CMD": {
                        String id = m.has("id") ? m.get("id").getAsString() : "";
                        String body = m.has("body") ? m.get("body").getAsString() : "";
                        onCommand(id, body);
                        break;
                    }
                    default:
                        Connector.LOGGER.warn("unknown frame type: {}", type);
                }
            }
        } catch (IOException e) {
            Connector.LOGGER.warn("Client IO ended: {}", e.toString());
        } finally {
            closeClient();
        }
    }

    private void onCommand(String id, String bodyUtf8) {
        String cmd = bodyUtf8.trim();
        MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
        if (server == null) {
            enqueueJson(json("type","ERR","id",id,"msg","server not ready"));
            return;
        }

        CompletableFuture<String> fut = new CompletableFuture<>();
        server.execute(() -> {
            try {
                String lower = cmd.toLowerCase();
                if (lower.startsWith("whitelist add ")) {
                    String playerName = cmd.substring("whitelist add ".length()).trim();
                    WhitelistHandler.addToWhitelist(server, playerName);
                    fut.complete("ok");
                } else if (lower.startsWith("unwhitelist ")) {
                    String playerName = cmd.substring("unwhitelist ".length()).trim();
                    WhitelistHandler.removeFromWhitelist(server, playerName);
                    fut.complete("ok");
                } else if (lower.startsWith("say ")) {
                    String msg = cmd.substring("say ".length());
                    server.getPlayerList().broadcastSystemMessage(Component.literal(msg), false);
                    fut.complete("ok");
                } else {
                    // Default: treat as plain chat broadcast
                    server.getPlayerList().broadcastSystemMessage(Component.literal(cmd), false);
                    fut.complete("ok");
                }
            } catch (Throwable t) {
                fut.completeExceptionally(t);
            }
        });

        try {
            String res = fut.get(5, TimeUnit.SECONDS);
            enqueueJson(json("type","RES","id",id,"body",res));
        } catch (Exception e) {
            String msg = e.getMessage() != null ? e.getMessage() : "error";
            enqueueJson(json("type","ERR","id",id,"msg",msg));
        }
    }

    // ---- outbound helpers (events + responses)

    // Back-compat: keep name but now emits EVT topic "chat" via NDJSON.
    public void sendToDiscord(String message) {
        sendEventString("chat", message);
    }

    public void sendEventString(String topic, String msg) {
        sendEvent(topic, msg.getBytes(StandardCharsets.UTF_8));
    }

    public void sendEvent(String topic, byte[] body) {
        String s = new String(body, StandardCharsets.UTF_8);
        JsonObject m = json("type","EVT","topic",topic,"body",s);
        enqueueJson(m);
    }

    private void enqueueJson(JsonObject m) {
        String line = gson.toJson(m) + "\n";
        if (!outbox.offer(line)) {
            Connector.LOGGER.warn("Outbox full; dropping message");
        }
    }

    private JsonObject json(Object... kv) {
        JsonObject o = new JsonObject();
        for (int i = 0; i + 1 < kv.length; i += 2) {
            String k = String.valueOf(kv[i]);
            Object v = kv[i+1];
            if (v == null) { o.addProperty(k, (String) null); continue; }
            if (v instanceof Number n) o.addProperty(k, n);
            else if (v instanceof Boolean b) o.addProperty(k, b);
            else o.addProperty(k, String.valueOf(v));
        }
        return o;
    }

    public boolean isConnected() {
        return clientSocket != null && clientSocket.isConnected() && !clientSocket.isClosed();
    }
}