// awiant/connector/PlayerJoinOrLeave.java
package awiant.connector;

import net.minecraft.server.level.ServerPlayer;
import net.minecraftforge.event.entity.player.PlayerEvent;
import net.minecraftforge.eventbus.api.SubscribeEvent;
import net.minecraftforge.fml.common.Mod;

@Mod.EventBusSubscriber
public class PlayerJoinOrLeave {
    @SubscribeEvent
    public static void onPlayerJoin(PlayerEvent.PlayerLoggedInEvent event) {
        if (!(event.getEntity() instanceof ServerPlayer player)) return;
        String playerName = player.getGameProfile().getName();
        Connector.LOGGER.info("{} joined the server", playerName);
        if (Connector.bridge != null) {
            Connector.bridge.sendEventString("join", "**" + playerName + "** joined the server.");
        }
    }

    @SubscribeEvent
    public static void onPlayerLeave(PlayerEvent.PlayerLoggedOutEvent event) {
        if (!(event.getEntity() instanceof ServerPlayer player)) return;
        String playerName = player.getGameProfile().getName();
        Connector.LOGGER.info("{} left the server", playerName);
        if (Connector.bridge != null) {
            Connector.bridge.sendEventString("leave", "**" + playerName + "** left the server.");
        }
    }
}
