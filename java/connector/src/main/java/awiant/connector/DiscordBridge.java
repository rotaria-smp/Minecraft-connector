package awiant.connector;

import net.minecraft.network.chat.Component;
import net.minecraft.server.MinecraftServer;
import net.minecraftforge.server.ServerLifecycleHooks;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.PrintWriter;
import java.net.ServerSocket;
import java.net.Socket;

public class DiscordBridge {
    private ServerSocket serverSocket;
    private Socket clientSocket;
    private PrintWriter out;
    private BufferedReader in;
    private final int port;

    public DiscordBridge(int port) {
        this.port = port;
        startServer();
    }

    private void startServer() {
        new Thread(() -> {
            try {
                serverSocket = new ServerSocket(port);
                Connector.LOGGER.info("Discord bridge listening on port " + port);

                clientSocket = serverSocket.accept();
                out = new PrintWriter(new java.io.OutputStreamWriter(clientSocket.getOutputStream(), java.nio.charset.StandardCharsets.UTF_8), true);
                in = new BufferedReader(new InputStreamReader(clientSocket.getInputStream(), java.nio.charset.StandardCharsets.UTF_8));

                // Listen for incoming messages from Discord bot
                new Thread(this::listenForMessages).start();

            } catch (IOException e) {
                Connector.LOGGER.error("Error in Discord bridge", e);
            }
        }).start();
    }

    // TODO: denna borde skicka till binärdata och inte strängar hit och dit
    public void sendToDiscord(String message) {
        if (out != null) {
            out.println(message);
        }
    }

    private void listenForMessages() {
        try {
            String inputLine;
            while ((inputLine = in.readLine()) != null) {
                if (inputLine.toLowerCase().startsWith("whitelist ")) {
                    String playerName = inputLine.substring("whitelist add".length()).trim();
                    MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
                    if (server != null && !playerName.isEmpty()) {
                        Connector.LOGGER.info("Adding player to whitelist: " + playerName);
                        WhitelistHandler.addToWhitelist(server, playerName);
                    }
                } else if (inputLine.toLowerCase().startsWith("unwhitelist ")) {
                    String playerName = inputLine.substring("unwhitelist ".length()).trim();
                    MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
                    if (server != null && !playerName.isEmpty()) {
                        Connector.LOGGER.info("Removing player from whitelist: " + playerName);
                        WhitelistHandler.removeFromWhitelist(server, playerName);
                    }
                }else {
                    MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
                    if (server != null) {
                        server.getPlayerList().broadcastSystemMessage(
                                Component.literal(inputLine), false);
                    }
                }
            }
        } catch (IOException e) {
            Connector.LOGGER.error("Error reading from Discord bot", e);
        }
    }

}