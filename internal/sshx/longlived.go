package sshx

import (
	"path/filepath"
	"strconv"
	"sync"
)

// longLivedPerMaster caps the channels on one long-lived ControlMaster below
// sshd's default MaxSessions (10). A refused channel makes ssh fall back to
// a fresh connection, which costs a full handshake.
const longLivedPerMaster = 8

// longLivedDir holds the long-lived masters' sockets, apart from the command
// master in cm/ so its recovery never touches them.
const longLivedDir = "cml"

// longLived counts open channels per machine and master shard.
type longLived struct {
	mu     sync.Mutex
	shards map[string][]int
}

// LongLived returns ssh options for a channel that stays open (a terminal
// attach, SFTP, a stream) and the func that frees its slot when the channel
// ends. Long-lived channels get their own sharded ControlMasters: sharing
// the command master would fill its MaxSessions and push every short
// command (list-sessions, new-session) onto a fresh ~0.4s handshake.
func (c *Client) LongLived(machine string) (opts []string, release func()) {
	c.long.mu.Lock()
	defer c.long.mu.Unlock()
	if c.long.shards == nil {
		c.long.shards = map[string][]int{}
	}
	counts := c.long.shards[machine]
	shard := len(counts)
	for i, n := range counts {
		if n < longLivedPerMaster {
			shard = i
			break
		}
	}
	if shard == len(counts) {
		counts = append(counts, 0)
	}
	counts[shard]++
	c.long.shards[machine] = counts
	var once sync.Once
	path := filepath.Join(c.cfg.Dir, longLivedDir, "%C-"+strconv.Itoa(shard))
	return []string{"-o", "ControlPath=" + path}, func() {
		once.Do(func() {
			c.long.mu.Lock()
			c.long.shards[machine][shard]--
			c.long.mu.Unlock()
		})
	}
}
