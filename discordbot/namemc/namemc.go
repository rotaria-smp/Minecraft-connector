package namemc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	http *http.Client

	url            string
	apiURL         string
	friendURL      string
	likeListURL    string
	uuidAPIURL     string
	skinURL        string
	capeURL        string
	userProfileURL string
	dropURL        string
}

func New() *Client {
	return &Client{
		http:           &http.Client{Timeout: 15 * time.Second},
		url:            "https://namemc.com",
		apiURL:         "https://api.mojang.com/users/profiles/minecraft/",
		friendURL:      "https://api.namemc.com/profile/",
		likeListURL:    "https://api.namemc.com/server/",
		uuidAPIURL:     "https://sessionserver.mojang.com/session/minecraft/profile/",
		skinURL:        "https://namemc.com/skin/",
		capeURL:        "https://namemc.com/cape/",
		userProfileURL: "https://namemc.com/profile/",
		dropURL:        "https://api.kqzz.me/api/namemc/droptime/",
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

// PrintFriendList returns a user's friends either by username or uuid.
// output should be "uuid" or "username" ("player" is treated as "username").
func (c *Client) PrintFriendList(playerUsername, playerUUID, output string) ([]string, error) {
	if output != "uuid" && output != "username" && output != "player" {
		return nil, errors.New(`output must be "uuid", "username", or "player"`)
	}
	if output == "player" {
		output = "username"
	}

	var uuid string
	var err error
	switch {
	case playerUUID != "":
		uuid = playerUUID
	case playerUsername != "":
		uuid, err = c.UsernameToUUID(playerUsername)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("either playerUsername or playerUUID is required")
	}

	type friend struct{ UUID, Name string }
	var friends []friend
	if err := c.getJSON(fmt.Sprintf("%s%s/friends", c.friendURL, uuid), &friends); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(friends))
	for _, f := range friends {
		if output == "uuid" {
			out = append(out, f.UUID)
		} else {
			out = append(out, f.Name)
		}
	}
	return out, nil
}

func (c *Client) AreFriends(uuid1, uuid2, username1, username2 string) (bool, error) {
	switch {
	case uuid1 != "" && uuid2 != "":
		var friends []struct {
			UUID string `json:"uuid"`
		}
		if err := c.getJSON(fmt.Sprintf("%s%s/friends", c.friendURL, uuid1), &friends); err != nil {
			return false, err
		}
		for _, f := range friends {
			if f.UUID == uuid2 {
				return true, nil
			}
		}
		return false, nil
	case username1 != "" && username2 != "":
		u1, err := c.UsernameToUUID(username1)
		if err != nil {
			return false, err
		}
		list, err := c.PrintFriendList("", u1, "username")
		if err != nil {
			return false, err
		}
		for _, n := range list {
			if n == username2 {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, errors.New("provide either both uuids or both usernames")
	}
}

func (c *Client) ServerLikeNumber(server string) (int, error) {
	if server == "" {
		return 0, errors.New("server required (include TLD)")
	}
	var likes []any
	if err := c.getJSON(fmt.Sprintf("%s%s/likes", c.likeListURL, server), &likes); err != nil {
		return 0, err
	}
	return len(likes), nil
}

func (c *Client) VerifyLike(server string, uuid, username string) (bool, error) {
	if server == "" {
		return false, errors.New("server required")
	}
	profile := uuid
	var err error
	if profile == "" && username != "" {
		profile, err = c.UsernameToUUID(username)
		if err != nil {
			return false, err
		}
	}
	if profile == "" {
		return false, errors.New("uuid or username required")
	}
	var liked bool
	url := fmt.Sprintf("%s%s/likes?profile=%s", c.likeListURL, server, profile)
	if err := c.getJSON(url, &liked); err != nil {
		return false, err
	}
	return liked, nil
}

// -------- scraping (std lib only; simple regex-based extraction) --------

// helper: extract inner HTML of <div class="...classSub..."> blocks
func findDivBlocks(html, classSub string) []string {
	pattern := fmt.Sprintf(`(?s)<div[^>]*class="[^"]*%s[^"]*"[^>]*>(.*?)</div>`, regexp.QuoteMeta(classSub))
	re := regexp.MustCompile(pattern)
	var out []string
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		out = append(out, m[1])
	}
	return out
}

// helper: extract anchor texts from a snippet
func anchorTexts(snippet string) []string {
	re := regexp.MustCompile(`(?s)<a[^>]*>(.*?)</a>`) // capture text between tags
	var out []string
	for _, m := range re.FindAllStringSubmatch(snippet, -1) {
		text := strings.TrimSpace(stripTags(m[1]))
		if text != "" {
			out = append(out, htmlUnescape(text))
		}
	}
	return out
}

// helper: extract anchor hrefs from a snippet
func anchorHrefs(snippet string) []string {
	re := regexp.MustCompile(`(?s)<a[^>]*href="([^"]+)"[^>]*>`) // capture href value
	var out []string
	for _, m := range re.FindAllStringSubmatch(snippet, -1) {
		h := strings.TrimSpace(m[1])
		if h != "" {
			out = append(out, h)
		}
	}
	return out
}

// very small HTML text stripper (std lib only)
func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]+>`) // remove tags
	return re.ReplaceAllString(s, "")
}

// unescape a few common HTML entities without external packages
func htmlUnescape(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&nbsp;", " ",
	)
	return replacer.Replace(s)
}

func (c *Client) SkinUsers(skinID string) ([]string, error) {
	if skinID == "" {
		return nil, errors.New("skinID required")
	}
	html, err := c.getText(c.skinURL + skinID)
	if err != nil {
		return nil, err
	}
	blocks := findDivBlocks(html, "card-body player-list py-2")
	var users []string
	for _, b := range blocks {
		texts := anchorTexts(b)
		for _, t := range texts {
			if t != "…" {
				users = append(users, t)
			}
		}
	}
	if len(users) == 0 {
		return nil, nil
	}
	return users, nil
}

func (c *Client) GetSkinTags(skinID string) ([]string, error) {
	if skinID == "" {
		return nil, errors.New("skinID required")
	}
	html, err := c.getText(c.skinURL + skinID)
	if err != nil {
		return nil, err
	}
	blocks := findDivBlocks(html, "card-body text-center py-1")
	var tags []string
	for _, b := range blocks {
		texts := anchorTexts(b)
		for _, t := range texts {
			if t != "" {
				tags = append(tags, t)
			}
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func (c *Client) GetSkinNumber(skinID string) (int, error) {
	users, err := c.SkinUsers(skinID)
	if err != nil {
		return 0, err
	}
	return len(users), nil
}

func (c *Client) GetCapeUsers(capeID string) ([]string, error) {
	if capeID == "" {
		return nil, errors.New("capeID required")
	}
	html, err := c.getText(c.capeURL + capeID)
	if err != nil {
		return nil, err
	}
	blocks := findDivBlocks(html, "card-body player-list py-2")
	var users []string
	for _, b := range blocks {
		texts := anchorTexts(b)
		for _, t := range texts {
			if t != "" && t != "…" {
				users = append(users, t)
			}
		}
	}
	return users, nil
}

func (c *Client) CapeUserNumber(capeHash string) (int, error) {
	if capeHash == "" {
		return 0, errors.New("capeHash required")
	}
	html, err := c.getText(c.capeURL + capeHash)
	if err != nil {
		return 0, err
	}
	blocks := findDivBlocks(html, "card-body player-list py-2")
	count := 0
	for _, b := range blocks {
		count += len(anchorTexts(b))
	}
	switch capeHash {
	case "1981aad373fa9754", "72ee2cfcefbfc081", "0e4cc75a5f8a886d", "ebc798c3f7eca2a3", "9349fa25c64ae935":
		if count > 0 {
			return count - 1, nil
		}
	}
	if count > 3000 {
		return count - 1, nil
	}
	return count, nil
}

// PlayerSkins returns skin hashes used on a profile. If current==true, returns only the current one.
func (c *Client) PlayerSkins(current bool, username, uuid string) ([]string, error) {
	var url string
	switch {
	case username != "":
		url = c.userProfileURL + username
	case uuid != "":
		url = c.userProfileURL + uuid
	default:
		return nil, errors.New("username or uuid required")
	}
	html, err := c.getText(url)
	if err != nil {
		return nil, err
	}
	blocks := findDivBlocks(html, "card-body text-center")
	var hrefs []string
	for _, b := range blocks {
		hrefs = append(hrefs, anchorHrefs(b)...)
	}
	var hashes []string
	for _, h := range hrefs {
		if h == "javascript:void(0)" {
			continue
		}
		hashes = append(hashes, strings.TrimPrefix(h, "/skin/"))
	}
	if current {
		if len(hashes) == 0 {
			return nil, nil
		}
		return []string{hashes[0]}, nil
	}
	return hashes, nil
}

func (c *Client) NameDrop(name string) (string, error) {
	if name == "" {
		return "", errors.New("name required")
	}
	body, err := c.getText(c.dropURL + name)
	if err != nil {
		return "", err
	}
	digits := strings.Builder{}
	for _, r := range body {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return "", errors.New("no timestamp found")
	}
	sec, err := strconv.ParseInt(digits.String(), 10, 64)
	if err != nil {
		return "", err
	}
	return time.Unix(sec, 0).Format("15:04:05"), nil
}

func (c *Client) RenderSkin(skinHash, model, x, y, direction, timeParam string) (string, error) {
	if skinHash == "" {
		return "", errors.New("skinHash required")
	}
	if model == "" {
		model = "big"
	}
	timeVal := "0"
	if timeParam != "" {
		timeVal = timeParam
	}
	if direction == "front" {
		return fmt.Sprintf("https://render.namemc.com/skin/3d/body.png?skin=%s&model=%s&theta=0&phi=0&time=%s&width=600&height=800", skinHash, model, timeVal), nil
	}
	if x != "" && y != "" {
		return fmt.Sprintf("https://render.namemc.com/skin/3d/body.png?skin=%s&model=%s&theta=%s&phi=%s&time=%s&width=600&height=800", skinHash, model, x, y, timeVal), nil
	}
	return fmt.Sprintf("https://render.namemc.com/skin/3d/body.png?skin=%s&model=%s&theta=0&phi=0&time=%s&width=600&height=800", skinHash, model, timeVal), nil
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

func (c *Client) getText(url string) (string, error) {
	resp, err := c.http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
