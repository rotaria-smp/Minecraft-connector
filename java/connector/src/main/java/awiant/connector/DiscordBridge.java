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
import java.util.concurrent.*;

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
                        writeImmediate(sess, json("type","PONG"));
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
            sess.stop();
            clearIfCurrent(sess);
        }
    }

    private void writeImmediate(ClientSession sess, JsonObject m) {
        try {
            byte[] bytes = (gson.toJson(m) + "\n").getBytes(StandardCharsets.UTF_8);
            synchronized (sess.out) {
                sess.out.write(bytes);
                sess.out.flush();
            }
        } catch (IOException ioe) {
            Connector.LOGGER.warn("immediate write failed (id={}) : {}", sess.id, ioe.toString());
        }
    }

    private void onCommand(ClientSession sess, String id, String bodyUtf8) {
        String cmd = bodyUtf8.trim();
        MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
        if (server == null) {
            sess.enqueueControl(json("type","ERR","id",id,"msg","server not ready"));
            return;
        }

        CompletableFuture<String> fut = new CompletableFuture<>();
        server.execute(() -> {
            try {
                String lower = cmd.toLowerCase();
                if (lower.startsWith("whitelist add ")) {
                    String playerName = cmd.substring("whitelist add ".length()).trim();
                    CommandHandler.addToWhitelist(server, playerName);
                    fut.complete("ok");
                } else if (lower.startsWith("unwhitelist ")) {
                    String playerName = cmd.substring("unwhitelist ".length()).trim();
                    CommandHandler.removeFromWhitelist(server, playerName);
                    fut.complete("ok");
                } else if (lower.startsWith("say ")) {
                    String msg = cmd.substring("say ".length());
                    server.getPlayerList().broadcastSystemMessage(Component.literal(msg), false);
                    fut.complete("ok");
                } else if (lower.startsWith("kick ")) {
                    String playerName = cmd.substring("kick ".length()).trim();
                    CommandHandler.kickPlayer(server, playerName);
                    fut.complete("ok");
                } else {
                    server.getPlayerList().broadcastSystemMessage(Component.literal(cmd), false);
                    fut.complete("ok");
                }
            } catch (Throwable t) {
                fut.completeExceptionally(t);
            }
        });

        try {
            String res = fut.get(5, TimeUnit.SECONDS);
            // Control path so RES is never stuck behind EVTs
            sess.enqueueControl(json("type","RES","id",id,"body",res));
        } catch (Exception e) {
            String msg = e.getMessage() != null ? e.getMessage() : "error";
            sess.enqueueControl(json("type","ERR","id",id,"msg",msg));
        }
    }

    // ---- outbound helpers (events + responses)

    public void sendToDiscord(String message) { sendEventString("chat", message); }

    public void sendEventString(String topic, String msg) { sendEvent(topic, msg.getBytes(StandardCharsets.UTF_8)); }

    public void sendEvent(String topic, byte[] body) {
        ClientSession s = session;
        if (s == null) return;
        String str = new String(body, StandardCharsets.UTF_8);
        // EVTs go to the normal queue; may be dropped when too full
        s.enqueue(json("type", "EVT", "topic", topic, "body", str));
        Connector.LOGGER.debug("send EVT (id={}) topic={} body={}", s.id, topic, str);
    }

    public boolean isConnected() {
        ClientSession s = session;
        return s != null && !s.socket.isClosed() && s.socket.isConnected();
    }

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
        // Separate queues: control (PONG/RES/ERR) and regular EVT
        final BlockingQueue<String> control = new LinkedBlockingQueue<>(1_000);
        final BlockingQueue<String> outbox = new LinkedBlockingQueue<>(10_000);
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
                        String line = control.poll(); // try control first
                        if (line == null) {
                            // wait a short time on regular queue; then re-check control
                            line = outbox.poll(200, TimeUnit.MILLISECONDS);
                            if (line == null) {
                                continue; // loop and poll control again
                            }
                        }
                        byte[] bytes = line.getBytes(StandardCharsets.UTF_8);
                        synchronized (out) {
                            out.write(bytes);
                            out.flush();
                        }
                    }
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                } catch (IOException ioe) {
                    Connector.LOGGER.warn("writer IO (id={}) : {}", id, ioe.toString());
                } finally {
                    try { socket.close(); } catch (IOException ignore) {}
                }
            }, "discord-bridge-writer-" + id);

            this.writer.setDaemon(true);
            this.writer.start();
        }

        void enqueue(JsonObject m) {
            String line = gson.toJson(m) + "\n";
            // If EVT queue is near full, drop newest EVT (shed load) to protect control frames
            if (!outbox.offer(line)) {
                Connector.LOGGER.warn("outbox full; dropping EVT (id={})", id);
            }
        }

        void enqueueControl(JsonObject m) {
            String line = gson.toJson(m) + "\n";
            // Control frames should almost never drop; wait briefly if needed
            try {
                if (!control.offer(line, 200, TimeUnit.MILLISECONDS)) {
                    Connector.LOGGER.warn("control queue saturated; dropping control frame (id={})", id);
                }
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }

        void stop() {
            writer.interrupt();
            control.clear();
            outbox.clear();
            try { socket.close(); } catch (IOException ignore) {}
        }
    }
}