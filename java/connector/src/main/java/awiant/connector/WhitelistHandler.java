package awiant.connector;

import com.mojang.authlib.GameProfile;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.players.PlayerList;
import net.minecraft.server.players.UserWhiteList;
import net.minecraft.server.players.UserWhiteListEntry;

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
}
