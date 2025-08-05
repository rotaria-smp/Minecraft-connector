package awiant.connector;

import net.minecraftforge.eventbus.api.SubscribeEvent;
import net.minecraftforge.fml.common.Mod;
import net.minecraftforge.event.entity.player.PlayerEvent;
import net.minecraftforge.eventbus.api.SubscribeEvent;
import net.minecraftforge.fml.common.Mod;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.network.chat.Component;

@Mod.EventBusSubscriber
public class PlayerJoinOrLeave {
        @SubscribeEvent
        public static void onPlayerJoin(PlayerEvent.PlayerLoggedInEvent event) {
            if (!(event.getEntity() instanceof ServerPlayer player)) return;

            String playerName = player.getGameProfile().getName();
            Connector.LOGGER.info(playerName + " joined the server");

            if (Connector.bridge != null) {
                Connector.bridge.sendToDiscord("**" + playerName + "** joined the server.");
            }
        }
    @SubscribeEvent
    public static void onPlayerLeave(PlayerEvent.PlayerLoggedOutEvent event) {
        if (!(event.getEntity() instanceof ServerPlayer player)) return;

        String playerName = player.getGameProfile().getName();
        Connector.LOGGER.info(playerName + " left the server");

        if (Connector.bridge != null) {
            Connector.bridge.sendToDiscord("**" + playerName + "** left the server.");
        }
    }
}
