package db

import (
	"fmt"
	"math/rand"
	"time"
)

var adjectives = []string{
	"brave", "calm", "clever", "cool", "eager",
	"fair", "fancy", "fast", "gentle", "happy",
	"jolly", "kind", "keen", "lucky", "merry",
	"neat", "nice", "proud", "quick", "quiet",
	"rapid", "sharp", "shiny", "smart", "smooth",
	"snowy", "soft", "solid", "spicy", "sunny",
	"super", "sweet", "swift", "tall", "tidy",
	"tiny", "warm", "wild", "wise", "witty",
}

var nouns = []string{
	"alpine", "anchor", "badger", "breeze", "brook",
	"canary", "canyon", "cedar", "cloud", "coral",
	"creek", "crystal", "dawn", "delta", "desert",
	"eagle", "ember", "falcon", "fern", "field",
	"finch", "flame", "forest", "frost", "garden",
	"glacier", "grove", "harbor", "hawk", "heron",
	"hill", "island", "jade", "jasper", "lake",
	"lantern", "lark", "leaf", "maple", "marsh",
	"meadow", "mesa", "mist", "moon", "moss",
	"oak", "ocean", "olive", "opal", "orchid",
	"otter", "owl", "palm", "panda", "pearl",
	"peak", "pebble", "pine", "pond", "prairie",
	"quartz", "rain", "raven", "reef", "river",
	"robin", "rock", "sage", "shore", "sky",
	"snow", "sparrow", "spring", "star", "stone",
	"storm", "stream", "summit", "sun", "swan",
	"thistle", "thunder", "tiger", "trail", "tree",
	"tulip", "valley", "violet", "wave", "willow",
	"wind", "wren", "zenith",
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

// GenerateLabel generates a memorable session label like "brave-tiger-1234".
func GenerateLabel() string {
	adj := adjectives[rand.Intn(len(adjectives))]
	noun := nouns[rand.Intn(len(nouns))]
	num := rand.Intn(9000) + 1000 // 1000-9999

	return fmt.Sprintf("%s-%s-%d", adj, noun, num)
}
