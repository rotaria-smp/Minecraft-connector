package namemc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	http *http.Client

	apiURL     string
	uuidAPIURL string
}

func New() *Client {
	return &Client{
		http:       &http.Client{Timeout: 15 * time.Second},
		apiURL:     "https://api.mojang.com/users/profiles/minecraft/",
		uuidAPIURL: "https://sessionserver.mojang.com/session/minecraft/profile/",
	}
}

func (c *Client) Version() string { return "1.4.1-go-stdlib" }

func (c *Client) UsernameToUUID(username string) (string, error) {
	if username == "" {
		return "", errors.New("username required")
	}
	url := fmt.Sprintf("%s%s?at=%d", c.apiURL, username, time.Now().Unix())
	var resp struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(url, &resp); err != nil {
		return "", err
	}
	if resp.ID == "" {
		return "", fmt.Errorf("uuid not found for %q", username)
	}
	return resp.ID, nil
}

func (c *Client) UUIDToUsername(uuid string) (string, error) {
	if uuid == "" {
		return "", errors.New("uuid required")
	}
	var resp struct {
		Name string `json:"name"`
	}
	if err := c.getJSON(c.uuidAPIURL+uuid, &resp); err != nil {
		return "", err
	}
	if resp.Name == "" {
		return "", fmt.Errorf("username not found for %q", uuid)
	}
	return resp.Name, nil
}

// ---------------- helpers ----------------

func (c *Client) getJSON(url string, out any) error {
	resp, err := c.http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
