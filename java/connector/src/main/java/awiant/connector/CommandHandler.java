package awiant.connector;

import com.mojang.authlib.GameProfile;
import net.minecraft.network.chat.Component;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.players.PlayerList;
import net.minecraft.server.players.UserWhiteList;
import net.minecraft.server.players.UserWhiteListEntry;

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
}
