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
    private volatile OutputStream out;
    private volatile BufferedReader headerIn;
    private volatile InputStream rawIn;
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
        out = s.getOutputStream();
        rawIn = s.getInputStream();
        headerIn = new BufferedReader(new InputStreamReader(rawIn, StandardCharsets.UTF_8));
    }

    private synchronized void closeClient() {
        try { if (clientSocket != null) clientSocket.close(); } catch (IOException ignore) {}
        clientSocket = null; out = null; rawIn = null; headerIn = null;
    }

    private void handleClient(Socket s) {
        try {
            for (;;) {
                String line = headerIn.readLine();
                if (line == null) break;

                if (line.equals("PING")) {
                    writeAscii("PONG\n");
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
                    int n = Integer.parseInt(parts[2]);
                    byte[] body = readN(rawIn, n);
                    onCommand(id, body);
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

    private void onCommand(String id, byte[] body) {
        String cmd = new String(body, StandardCharsets.UTF_8).trim();
        MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
        if (server == null) {
            sendErr(id, "server not ready");
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
                } else {
                    // Default: broadcast the payload as a server message
                    server.getPlayerList().broadcastSystemMessage(Component.literal(cmd), false);
                    fut.complete("ok");
                }
            } catch (Throwable t) {
                fut.completeExceptionally(t);
            }
        });

        try {
            String res = fut.get(5, TimeUnit.SECONDS);
            sendRes(id, res.getBytes(StandardCharsets.UTF_8));
        } catch (Exception e) {
            sendErr(id, e.getMessage() != null ? e.getMessage() : "error");
        }
    }

    // ---- outbound helpers (events + responses)

    // Keep old name: now emits EVT "chat" so your existing callers still work.
    public void sendToDiscord(String message) {
        sendEventString("chat", message);
    }

    public void sendEventString(String topic, String msg) {
        sendEvent(topic, msg.getBytes(StandardCharsets.UTF_8));
    }

    public void sendEvent(String topic, byte[] body) {
        if (out == null) return;
        String header = "EVT " + topic + " " + body.length + "\n";
        writeFrame(header, body);
    }

    private void sendRes(String id, byte[] body) {
        writeFrame("RES " + id + " " + body.length + "\n", body);
    }

    private void sendErr(String id, String msg) {
        byte[] b = msg.getBytes(StandardCharsets.UTF_8);
        writeFrame("ERR " + id + " " + b.length + "\n", b);
    }

    private synchronized void writeFrame(String header, byte[] body) {
        try {
            if (out == null) return;
            out.write(header.getBytes(StandardCharsets.UTF_8));
            out.write(body);
            out.flush();
        } catch (IOException e) {
            Connector.LOGGER.error("write failed", e);
            closeClient();
        }
    }

    private synchronized void writeAscii(String s) throws IOException {
        if (out == null) return;
        out.write(s.getBytes(StandardCharsets.UTF_8));
        out.flush();
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
