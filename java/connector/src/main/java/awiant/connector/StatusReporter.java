package awiant.connector;

import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerPlayer;
import net.minecraftforge.event.TickEvent;
import net.minecraftforge.eventbus.api.SubscribeEvent;
import net.minecraftforge.fml.common.Mod;
import net.minecraftforge.server.ServerLifecycleHooks;

import java.util.List;
import java.util.stream.Collectors;

@Mod.EventBusSubscriber
public class StatusReporter {

    private static int tickCounter = 0;

    @SubscribeEvent
    public static void onServerTick(TickEvent.ServerTickEvent event) {
        if (event.phase != TickEvent.Phase.END) return;

        tickCounter++;
        if (tickCounter >= 200) { // ~10s
            tickCounter = 0;
            reportStatus();
        }
    }

    private static void reportStatus() {
        MinecraftServer server = ServerLifecycleHooks.getCurrentServer();
        if (server == null) return;

        double meanTickTime = server.getAverageTickTime();
        double tps = Math.min(1000.0 / meanTickTime, 20.0);

        int playerCount = server.getPlayerList().getPlayerCount();
        List<ServerPlayer> players = server.getPlayerList().getPlayers();
        String playerNames = players.stream()
                .map(p -> p.getGameProfile().getName())
                .collect(Collectors.joining(", "));

        String statusMessage = String.format("[UPDATE] TPS: %.2f | Online: %d%s",
                tps, playerCount, playerCount > 0 ? (" | Players: " + playerNames) : "");

        if (Connector.bridge != null) {
            Connector.bridge.sendEventString("status", statusMessage);
        }
    }
}