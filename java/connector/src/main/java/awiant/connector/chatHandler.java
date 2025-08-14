package awiant.connector;

import net.minecraft.server.level.ServerPlayer;
import net.minecraftforge.event.ServerChatEvent;
import net.minecraftforge.eventbus.api.SubscribeEvent;

public class chatHandler {
    @SubscribeEvent
    public void onServerChat(ServerChatEvent event) {
        ServerPlayer player = event.getPlayer();
        String message = String.format("<%s> %s",
                player.getDisplayName().getString(),
                event.getMessage().getString());
        Connector.bridge.sendToDiscord(message);
    }
}
