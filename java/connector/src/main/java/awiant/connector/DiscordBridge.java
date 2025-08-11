package awiant.connector;

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
    // Only used for broadcasting EVT frames to the *current* client
    private volatile OutputStream eventOut;
    private final int port;

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
            Connector.LOGGER.info("Discord bridge listening on port {}", port);
            while (!serverSocket.isClosed()) {
                Socket s = serverSocket.accept();
                s.setTcpNoDelay(true);
                replaceClient(s);
                new Thread(() -> handleClient(s), "discord-bridge-client").start();
            }
        } catch (IOException e) {
            Connector.LOGGER.error("Error in Discord bridge", e);
        }
    }

    private synchronized void replaceClient(Socket s) throws IOException {
        closeClient();
        clientSocket = s;
        eventOut = s.getOutputStream(); // for EVT broadcasts
    }

    private synchronized void closeClient() {
        try { if (clientSocket != null) clientSocket.close(); } catch (IOException ignore) {}
        clientSocket = null; eventOut = null;
    }

    private void handleClient(Socket s) {
        try (InputStream rin = s.getInputStream();
             OutputStream rout = s.getOutputStream();
             BufferedReader hin = new BufferedReader(new InputStreamReader(rin, StandardCharsets.UTF_8))) {

            for (;;) {
                String line = hin.readLine();
                if (line == null) break;

                if (line.equals("PING")) {
                    writeAsciiLocal(rout, "PONG\n");
                    continue;
                }

                String[] parts = line.split(" ", 3);
                if (parts.length < 3) {
                    Connector.LOGGER.warn("Bad frame: {}", line);
                    break;
                }

                String kind = parts[0];
                if ("CMD".equals(kind)) {
                    String id = parts[1];
                    int n;
                    try { n = Integer.parseInt(parts[2]); } catch (NumberFormatException e) { Connector.LOGGER.warn("Bad length in frame: {}", line); break; }
                    if (n < 0 || n > (16 << 20)) { Connector.LOGGER.warn("Suspicious length: {}", n); break; }
                    byte[] body = readN(rin, n);
                    onCommand(rout, id, body);
                } else {
                    Connector.LOGGER.warn("Unknown frame: {}", line);
                }
            }
        } catch (IOException e) {
            Connector.LOGGER.warn("Client IO ended: {}", e.toString());
        } finally {
            closeClient();
        }
    }

    private void onCommand(OutputStream rout, String id, byte[] body) {
        String cmd = new String(body, StandardCharsets.UTF_8).trim();
        MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
        if (server == null) {
            sendErrLocal(rout, id, "server not ready");
            return;
        }

        // Run MC changes on the main thread; then respond
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
            sendResLocal(rout, id, res.getBytes(StandardCharsets.UTF_8));
        } catch (Exception e) {
            sendErrLocal(rout, id, e.getMessage() != null ? e.getMessage() : "error");
        }
    }

    // ---- outbound helpers (events + responses)

    // Back-compat: keep name but now emits EVT topic "chat".
    public void sendToDiscord(String message) {
        sendEventString("chat", message);
    }

    public void sendEventString(String topic, String msg) {
        sendEvent(topic, msg.getBytes(StandardCharsets.UTF_8));
    }

    public void sendEvent(String topic, byte[] body) {
        String header = "EVT " + topic + " " + body.length + "\n";
        writeEventFrame(header, body);
    }

    private void sendResLocal(OutputStream o, String id, byte[] body) {
        writeFrameLocal(o, "RES " + id + " " + body.length + "\n", body);
    }

    private void sendErrLocal(OutputStream o, String id, String msg) {
        byte[] b = msg.getBytes(StandardCharsets.UTF_8);
        writeFrameLocal(o, "ERR " + id + " " + b.length + "\n", b);
    }

    private void writeFrameLocal(OutputStream o, String header, byte[] body) {
        try {
            o.write(header.getBytes(StandardCharsets.UTF_8));
            o.write(body);
            o.flush();
        } catch (IOException e) {
            Connector.LOGGER.error("write failed", e);
        }
    }

    private synchronized void writeEventFrame(String header, byte[] body) {
        try {
            if (eventOut == null) return;
            eventOut.write(header.getBytes(StandardCharsets.UTF_8));
            eventOut.write(body);
            eventOut.flush();
        } catch (IOException e) {
            Connector.LOGGER.error("event write failed", e);
            closeClient();
        }
    }

    private void writeAsciiLocal(OutputStream o, String s) throws IOException {
        o.write(s.getBytes(StandardCharsets.UTF_8));
        o.flush();
    }

    private static byte[] readN(InputStream in, int n) throws IOException {
        byte[] buf = new byte[n];
        int off = 0;
        while (off < n) {
            int r = in.read(buf, off, n - off);
            if (r < 0) throw new EOFException("stream closed");
            off += r;
        }
        return buf;
    }

    public boolean isConnected() {
        return clientSocket != null && clientSocket.isConnected() && !clientSocket.isClosed();
    }
}