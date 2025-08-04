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
                out = new PrintWriter(clientSocket.getOutputStream(), true);
                in = new BufferedReader(new InputStreamReader(clientSocket.getInputStream()));

                // Listen for incoming messages from Discord bot
                new Thread(this::listenForMessages).start();

            } catch (IOException e) {
                Connector.LOGGER.error("Error in Discord bridge", e);
            }
        }).start();
    }

    public void sendToDiscord(String message) {
        if (out != null) {
            out.println(message);
        }
    }

    private void listenForMessages() {
        try {
            String inputLine;
            while ((inputLine = in.readLine()) != null) {
                // Broadcast messages from Discord to Minecraft
                MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
                if (server != null) {
                    server.getPlayerList().broadcastSystemMessage(
                            Component.literal("[Discord] " + inputLine), false);
                }
            }
        } catch (IOException e) {
            Connector.LOGGER.error("Error reading from Discord bot", e);
        }
    }
}