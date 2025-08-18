package awiant.connector;

import com.mojang.authlib.GameProfile;
import net.minecraft.commands.CommandSource;
import net.minecraft.commands.CommandSourceStack;
import net.minecraft.network.chat.Component;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.players.PlayerList;
import net.minecraft.server.players.UserWhiteList;
import net.minecraft.server.players.UserWhiteListEntry;
import net.minecraft.world.phys.Vec2;
import net.minecraft.world.phys.Vec3;

import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;

public class CommandHandler {

    public static void addToWhitelist(MinecraftServer server, String playerName) {
        PlayerList playerList = server.getPlayerList();
        UserWhiteList whitelist = playerList.getWhiteList();

        GameProfile profile = server.getProfileCache().get(playerName).orElse(null);

        if (profile != null && profile.getId() != null) {
            whitelist.add(new UserWhiteListEntry(profile));
            try {
                whitelist.save();
            } catch (Exception e) {
                e.printStackTrace();
            }
        } else {
            CompletableFuture.runAsync(() -> {
                GameProfile fetchedProfile = server.getSessionService()
                        .fillProfileProperties(new GameProfile(null, playerName), true);

                if (fetchedProfile != null && fetchedProfile.getId() != null) {
                    server.execute(() -> {
                        whitelist.add(new UserWhiteListEntry(fetchedProfile));
                        try {
                            whitelist.save();
                            Connector.LOGGER.info("Added " + playerName + " to whitelist (Mojang lookup)");
                        } catch (Exception e) {
                            Connector.LOGGER.error("Could not save whitelist after adding " + playerName, e);
                        }
                    });
                } else {
                    Connector.LOGGER.warn("Could not resolve UUID for player '" + playerName + "'. Skipping whitelist add.");
                }
            });
        }
    }

    public static void removeFromWhitelist(MinecraftServer server, String playerName) {
        PlayerList playerList = server.getPlayerList();
        UserWhiteList whitelist = playerList.getWhiteList();

        Optional<GameProfile> cachedProfile = server.getProfileCache().get(playerName);

        if (cachedProfile.isPresent()) {
            GameProfile profile = cachedProfile.get();
            whitelist.remove(new UserWhiteListEntry(profile));
            try {
                whitelist.save();
                Connector.LOGGER.info("Removed " + playerName + " from the whitelist (cache hit)");
            } catch (Exception e) {
                Connector.LOGGER.error("Could not save whitelist after removing " + playerName, e);
            }
        } else {
            CompletableFuture.runAsync(() -> {
                GameProfile fetchedProfile = server.getSessionService()
                        .fillProfileProperties(new GameProfile(null, playerName), true);

                if (fetchedProfile != null && fetchedProfile.getId() != null) {
                    server.execute(() -> {
                        whitelist.remove(new UserWhiteListEntry(fetchedProfile));
                        try {
                            whitelist.save();
                            Connector.LOGGER.info("Removed " + playerName + " from the whitelist (Mojang lookup)");
                        } catch (Exception e) {
                            Connector.LOGGER.error("Could not save whitelist after removing " + playerName, e);
                        }
                    });
                } else {
                    Connector.LOGGER.warn("Could not find a valid profile for " + playerName + " to remove from whitelist");
                }
            });
        }
    }
    public static void kickPlayer(MinecraftServer server, String playerName) {
        PlayerList playerList = server.getPlayerList();
        ServerPlayer player = playerList.getPlayerByName(playerName);

        if (player != null) {
            try {
                player.connection.disconnect(Component.literal("You have been kicked from the server. Reason: Word in blacklist, please do not use it"));
            } catch (Exception e) {
                Connector.LOGGER.error("Could not kick " + playerName + ": " + e.getMessage(), e);
            }
        } else {
            Connector.LOGGER.warn("Player " + playerName + " not found or not online.");
        }
    }

    public static List<String> ExecuteCommand(MinecraftServer server, String command) {
        CapturingCommandSource cap = new CapturingCommandSource();
        CommandSourceStack source = new CommandSourceStack(
                cap,                        // no entity, NULL works
                new Vec3(0, 64, 0),                        // position in the world
                Vec2.ZERO,                                 // rotation
                server.overworld(),                        // dimension / level
                0,                                         // permission level (0 = non-op)
                "System",                                  // name
                Component.literal("System"),               // display name
                server,                                    // server instance
                null                                       // no entity context
        );
        Connector.LOGGER.debug("Executing command {}", command);
        int result = server.getCommands().performPrefixedCommand(source, command);

        Connector.LOGGER.debug("Result code: {}", result);
        cap.getMessages().forEach(msg ->
                Connector.LOGGER.debug("Captured: {}", msg.getString())
        );

        // Convert captured messages to plain strings
        List<String> out = new ArrayList<>();
        for (Component c : cap.getMessages()) {
            out.add(c.getString()); // or use Component.Serializer.toJson(c) if you want formatting
        }

        return out;
    }

    private static  class  CapturingCommandSource implements CommandSource {
        private final List<Component> messages = new ArrayList<>();

        @Override
        public void sendSystemMessage(Component component) {
            messages.add(component);
        }

        @Override
        public boolean acceptsSuccess() {
            return true; // capture success messages
        }

        @Override
        public boolean acceptsFailure() {
            return true; // capture failure messages
        }

        @Override
        public boolean shouldInformAdmins() {
            return false; // don’t spam console/log
        }

        public List<Component> getMessages() {
            return messages;
        }
    }
}
