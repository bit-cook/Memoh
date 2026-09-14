package sticker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/felinics/memoh/internal/config"
	memslug "github.com/felinics/memoh/internal/memory/slug"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

const (
	// defaultSearchLimit keeps a search result small enough to read in one
	// tool response; maxSearchLimit stops a library dump from evicting the
	// conversation it was supposed to help with.
	defaultSearchLimit = 10
	maxSearchLimit     = 50

	// shortIDLength is how much of the platform's unique id goes into the file
	// name. Long enough to not collide within one bot's library, short enough
	// that the name still reads as the pack it belongs to.
	shortIDLength = 10

	overviewHeader = "# Sticker Library\n\n" +
		"<!-- Generated from stickers/*.md. Edit an entry file to change a description; this list is rewritten on every save. -->\n"
)

// Service reads and writes the sticker library in a bot's workspace.
type Service struct {
	provider bridge.Provider
	logger   *slog.Logger
	now      func() time.Time
}

func New(log *slog.Logger, provider bridge.Provider) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		provider: provider,
		logger:   log.With(slog.String("component", "sticker")),
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func stickerDirPath() string { return path.Join(config.DefaultDataMount, "stickers") }
func stickerOverviewPath() string {
	return path.Join(config.DefaultDataMount, "STICKERS.md")
}

// entryFileName derives a stable, human-readable file name. The unique id
// suffix is what makes it stable: the pack name can be renamed upstream or
// missing entirely, and two stickers in one pack share everything else.
func entryFileName(entry Entry) string {
	base := memslug.Slugify(entry.Pack)
	if base == "" {
		base = "misc"
	}
	return base + "-" + shortID(entry.Identity()) + ".md"
}

func shortID(id string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(id)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
		if sb.Len() >= shortIDLength {
			break
		}
	}
	if sb.Len() == 0 {
		return "unknown"
	}
	return sb.String()
}

// List returns every stored sticker, newest update first.
func (s *Service) List(ctx context.Context, botID string) ([]Entry, error) {
	if s == nil || s.provider == nil {
		return nil, ErrNotConfigured
	}
	client, err := s.provider.MCPClient(ctx, botID)
	if err != nil {
		return nil, err
	}
	files, err := client.ListDirAll(ctx, stickerDirPath(), true)
	if err != nil {
		if errors.Is(err, bridge.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	entries := make([]Entry, 0, len(files))
	for _, file := range files {
		filePath := file.GetPath()
		if file.GetIsDir() || !strings.HasSuffix(filePath, ".md") {
			continue
		}
		content, err := readFile(ctx, client, filePath)
		if err != nil {
			// One unreadable file must not hide the rest of the library: the
			// agent edits these by hand, so a broken entry is a normal state.
			s.logger.Warn("read sticker entry failed", slog.String("path", filePath), slog.Any("error", err))
			continue
		}
		entry, err := parseEntry(content)
		if err != nil {
			s.logger.Warn("parse sticker entry failed", slog.String("path", filePath), slog.Any("error", err))
			continue
		}
		if strings.TrimSpace(entry.Ref) == "" {
			continue
		}
		entry.Path = filePath
		entries = append(entries, entry)
	}
	sortEntries(entries)
	return entries, nil
}

// Search returns stored stickers matching every token in query. An empty
// query lists the library.
func (s *Service) Search(ctx context.Context, botID, query string, limit int) ([]Entry, error) {
	entries, err := s.List(ctx, botID)
	if err != nil {
		return nil, err
	}
	return filterEntries(entries, query, limit), nil
}

// FindByRef looks up a sticker by its platform reference or unique id.
func (s *Service) FindByRef(ctx context.Context, botID, ref string) (Entry, bool, error) {
	entries, err := s.List(ctx, botID)
	if err != nil {
		return Entry{}, false, err
	}
	entry, ok := findByRef(entries, ref)
	return entry, ok, nil
}

// Save writes a sticker and its description, merging with an existing entry
// for the same sticker, then rewrites the overview.
func (s *Service) Save(ctx context.Context, botID string, incoming Entry) (Entry, error) {
	if s == nil || s.provider == nil {
		return Entry{}, ErrNotConfigured
	}
	now := s.now()
	incoming = incoming.normalized(now)
	if incoming.Ref == "" {
		return Entry{}, ErrRefRequired
	}
	if incoming.Description == "" {
		return Entry{}, ErrDescRequired
	}
	entries, err := s.List(ctx, botID)
	if err != nil {
		return Entry{}, err
	}

	saved := incoming
	index := -1
	if existing, pos := findEntryIndex(entries, incoming); pos >= 0 {
		saved = existing.merge(incoming, now)
		saved.Path = existing.Path
		index = pos
	}
	if strings.TrimSpace(saved.Path) == "" {
		saved.Path = path.Join(stickerDirPath(), entryFileName(saved))
	}

	content, err := formatEntry(saved)
	if err != nil {
		return Entry{}, err
	}
	client, err := s.provider.MCPClient(ctx, botID)
	if err != nil {
		return Entry{}, err
	}
	if err := client.WriteFile(ctx, saved.Path, []byte(content)); err != nil {
		return Entry{}, fmt.Errorf("write sticker entry: %w", err)
	}

	if index >= 0 {
		entries[index] = saved
	} else {
		entries = append(entries, saved)
	}
	sortEntries(entries)
	if err := client.WriteFile(ctx, stickerOverviewPath(), []byte(formatOverview(entries))); err != nil {
		// The entry is already durable and the overview is derived from it, so
		// a failed rewrite is stale-index territory, not lost work.
		s.logger.Warn("write sticker overview failed", slog.Any("error", err))
	}
	return saved, nil
}

func readFile(ctx context.Context, client *bridge.Client, filePath string) (string, error) {
	reader, err := client.ReadRaw(ctx, filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func findEntryIndex(entries []Entry, target Entry) (Entry, int) {
	identity := target.Identity()
	for i, entry := range entries {
		if identity != "" && entry.Identity() == identity {
			return entry, i
		}
		if entry.Ref != "" && entry.Ref == target.Ref {
			return entry, i
		}
	}
	return Entry{}, -1
}

func findByRef(entries []Entry, ref string) (Entry, bool) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return Entry{}, false
	}
	for _, entry := range entries {
		if entry.Ref == trimmed || entry.UniqueID == trimmed {
			return entry, true
		}
	}
	return Entry{}, false
}

func filterEntries(entries []Entry, query string, limit int) []Entry {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	tokens := queryTokens(query)
	matched := make([]Entry, 0, limit)
	for _, entry := range entries {
		if !matchTokens(entry, tokens) {
			continue
		}
		matched = append(matched, entry)
		if len(matched) == limit {
			break
		}
	}
	return matched
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].UpdatedAt.Equal(entries[j].UpdatedAt) {
			return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
		}
		return entries[i].Identity() < entries[j].Identity()
	})
}

// formatOverview renders the browsable index. It is grouped by pack because
// that is how a sticker library is actually used: the agent reaches for a
// voice ("the cat pack"), not for a timestamp.
func formatOverview(entries []Entry) string {
	byPack := map[string][]Entry{}
	for _, entry := range entries {
		pack := entry.Pack
		if pack == "" {
			pack = "Unpacked"
		}
		byPack[pack] = append(byPack[pack], entry)
	}
	packs := make([]string, 0, len(byPack))
	for pack := range byPack {
		packs = append(packs, pack)
	}
	sort.Strings(packs)

	var sb strings.Builder
	sb.WriteString(overviewHeader)
	if len(entries) == 0 {
		sb.WriteString("\nNo stickers saved yet.\n")
		return sb.String()
	}
	for _, pack := range packs {
		fmt.Fprintf(&sb, "\n## %s\n\n", pack)
		items := byPack[pack]
		sort.SliceStable(items, func(i, j int) bool { return items[i].Identity() < items[j].Identity() })
		for _, entry := range items {
			prefix := entry.Emoji
			if prefix == "" {
				prefix = "•"
			}
			fmt.Fprintf(&sb, "- %s [%s](%s)\n", prefix, overviewSummary(entry), overviewLink(entry))
		}
	}
	return sb.String()
}

// overviewSummary keeps one entry to one line. Markdown link text cannot hold
// a newline or an unescaped bracket without breaking the link.
func overviewSummary(entry Entry) string {
	summary := strings.TrimSpace(entry.Description)
	if idx := strings.IndexAny(summary, "\n\r"); idx >= 0 {
		summary = strings.TrimSpace(summary[:idx])
	}
	summary = strings.NewReplacer("[", "(", "]", ")").Replace(summary)
	if summary == "" {
		summary = "(no description)"
	}
	const maxSummaryRunes = 80
	runes := []rune(summary)
	if len(runes) > maxSummaryRunes {
		summary = string(runes[:maxSummaryRunes]) + "…"
	}
	return summary
}

func overviewLink(entry Entry) string {
	name := path.Base(strings.TrimSpace(entry.Path))
	if name == "" || name == "." || name == "/" {
		name = entryFileName(entry)
	}
	return "stickers/" + name
}
