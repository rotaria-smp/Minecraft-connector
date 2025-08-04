package awiant.connector;

import com.mojang.authlib.GameProfile;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.players.PlayerList;
import net.minecraft.server.players.UserWhiteList;
import net.minecraft.server.players.UserWhiteListEntry;

import java.util.Optional;
import java.util.concurrent.CompletableFuture;

public class WhitelistHandler {

    public static void addToWhitelist(MinecraftServer server, String playerName) {
        PlayerList playerList = server.getPlayerList();
        UserWhiteList whitelist = playerList.getWhiteList();

        GameProfile profile = server.getProfileCache().get(playerName).orElse(null);

        if (profile != null) {
            whitelist.add(new UserWhiteListEntry(profile));
            try {
                whitelist.save();
            } catch (Exception e) {
                e.printStackTrace();
            }
        } else {
            CompletableFuture.runAsync(() -> {
                GameProfile fetchedProfile = server.getSessionService().fillProfileProperties(new GameProfile(null, playerName), true);
                if (fetchedProfile != null) {
                    server.execute(() -> {
                        whitelist.add(new UserWhiteListEntry(fetchedProfile));
                        try {
                            whitelist.save();
                        } catch (Exception e) {
                            e.printStackTrace();
                        }
                    });
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
}
