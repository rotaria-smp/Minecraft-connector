package awiant.connector;

import net.minecraftforge.event.server.ServerStartedEvent;
import net.minecraftforge.event.server.ServerStoppingEvent;
import net.minecraftforge.eventbus.api.SubscribeEvent;
import net.minecraftforge.fml.common.Mod;
@Mod.EventBusSubscriber
public class ServerLifecycleEvents {

    @SubscribeEvent
    public static void onServerStarted(ServerStartedEvent event) {
        Connector.bridge.sendToDiscord("✅ Minecraft server is now **online**.");
    }

    @SubscribeEvent
    public static void onServerStopping(ServerStoppingEvent event) {
        Connector.bridge.sendToDiscord("❌ Minecraft server is shutting down.");
    }
}
