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
    private final int port;
    private final Gson gson = new Gson();

    private final java.util.concurrent.atomic.AtomicLong gen = new java.util.concurrent.atomic.AtomicLong();
    private volatile ClientSession session; // the single active connection

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
                Connector.LOGGER.info("accepted {}", s);
                ClientSession newSession = replaceClient(s);
                new Thread(() -> handleClient(newSession), "discord-bridge-client-" + newSession.id).start();
            }
        } catch (IOException e) {
            Connector.LOGGER.error("Error in Discord bridge", e);
        }
    }

    private synchronized ClientSession replaceClient(Socket s) throws IOException {
        // Preempt any existing client
        if (session != null) {
            Connector.LOGGER.warn("Preempting existing client (id={}) with new connection", session.id);
            session.stop();
        }
        session = new ClientSession(gen.incrementAndGet(), s);
        return session;
    }

    private synchronized void clearIfCurrent(ClientSession maybeCurrent) {
        if (session == maybeCurrent) {
            session = null;
        }
    }

    private void handleClient(ClientSession sess) {
        try (InputStream rin = sess.socket.getInputStream();
             BufferedReader hin = new BufferedReader(new InputStreamReader(rin, StandardCharsets.UTF_8))) {

            for (;;) {
                String line = hin.readLine();
                if (line == null) {
                    Connector.LOGGER.info("client EOF (id={})", sess.id);
                    break;
                }
                line = line.trim();
                if (line.isEmpty()) continue;

                Connector.LOGGER.debug("recv json (id={}): {}", sess.id, line);
                JsonObject m;
                try {
                    m = gson.fromJson(line, JsonObject.class);
                } catch (com.google.gson.JsonSyntaxException ex) {
                    Connector.LOGGER.warn("bad json syntax (id={}): {}", sess.id, line);
                    continue;
                }
                if (m == null || !m.has("type")) {
                    Connector.LOGGER.warn("bad json frame (id={}): {}", sess.id, line);
                    continue;
                }

                String type = m.get("type").getAsString();
                switch (type) {
                    case "PING":
                        sess.enqueue(json("type","PONG"));
                        break;

                    case "CMD": {
                        String id = m.has("id") ? m.get("id").getAsString() : "";
                        String body = m.has("body") ? m.get("body").getAsString() : "";
                        onCommand(sess, id, body);
                        break;
                    }

                    default:
                        Connector.LOGGER.warn("unknown frame type (id={}): {}", sess.id, type);
                }
            }
        } catch (IOException e) {
            Connector.LOGGER.warn("Client IO ended (id={}): {}", sess.id, e.toString());
        } finally {
            // Only tear down the global session if this handler still owns it
            sess.stop();
            clearIfCurrent(sess);
        }
    }

    private void onCommand(ClientSession sess, String id, String bodyUtf8) {
        String cmd = bodyUtf8.trim();
        MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
        if (server == null) {
            sess.enqueue(json("type","ERR","id",id,"msg","server not ready"));
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
            sess.enqueue(json("type","RES","id",id,"body",res));
        } catch (Exception e) {
            String msg = e.getMessage() != null ? e.getMessage() : "error";
            sess.enqueue(json("type","ERR","id",id,"msg",msg));
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
        ClientSession s = session;
        if (s == null) return;
        String str = new String(body, StandardCharsets.UTF_8);
        s.enqueue(json("type","EVT","topic",topic,"body",str));
    }

    public boolean isConnected() {
        ClientSession s = session;
        return s != null && !s.socket.isClosed() && s.socket.isConnected();
    }

    // ---- utilities

    private JsonObject json(Object... kv) {
        JsonObject o = new JsonObject();
        for (int i = 0; i + 1 < kv.length; i += 2) {
            String k = String.valueOf(kv[i]);
            Object v = kv[i + 1];
            if (v == null) { o.addProperty(k, (String) null); continue; }
            if (v instanceof Number n) o.addProperty(k, n);
            else if (v instanceof Boolean b) o.addProperty(k, b);
            else o.addProperty(k, String.valueOf(v));
        }
        return o;
    }

    private final class ClientSession {
        final long id;
        final Socket socket;
        final OutputStream out;
        final java.util.concurrent.BlockingQueue<String> outbox =
                new java.util.concurrent.LinkedBlockingQueue<>(10_000);
        final Thread writer;

        ClientSession(long id, Socket socket) throws IOException {
            this.id = id;
            this.socket = socket;
            this.socket.setTcpNoDelay(true);
            this.socket.setKeepAlive(true);
            try { this.socket.setSendBufferSize(256 * 1024); } catch (Exception ignore) {}
            this.out = socket.getOutputStream();

            this.writer = new Thread(() -> {
                try {
                    for (;;) {
                        String line = outbox.take(); // NDJSON line (ends with '\n')
                        byte[] bytes = line.getBytes(StandardCharsets.UTF_8);
                        out.write(bytes);
                        out.flush();
                    }
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                } catch (IOException ioe) {
                    Connector.LOGGER.warn("writer IO (id={}): {}", id, ioe.toString());
                } finally {
                    try { socket.close(); } catch (IOException ignore) {}
                }
            }, "discord-bridge-writer-" + id);

            this.writer.setDaemon(true);
            this.writer.start();
        }

        void enqueue(JsonObject m) {
            String line = gson.toJson(m) + "\n";
            if (!outbox.offer(line)) {
                Connector.LOGGER.warn("outbox full; dropping (id={})", id);
            }
        }

        void stop() {
            writer.interrupt();
            outbox.clear();
            try { socket.close(); } catch (IOException ignore) {}
        }
    }
}
