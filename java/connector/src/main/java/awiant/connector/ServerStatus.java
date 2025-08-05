package awiant.connector;

// TODO: Bör användas för att skicka serverstatus till Discord istället för att skicka string direkt
public class ServerStatus {
    private final float tps;
    private final int playerCount;

    public ServerStatus(float tps, int playerCount) {
        this.tps = tps;
        this.playerCount = playerCount;
    }

    public float getTps() {
        return tps;
    }

    public int getPlayerCount() {
        return playerCount;
    }
}