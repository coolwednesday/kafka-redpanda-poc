package payload

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

type MessageType string

const (
	OrderPlaced         MessageType = "OrderPlaced"
	PaymentSettled      MessageType = "PaymentSettled"
	InventoryAdjusted   MessageType = "InventoryAdjusted"
	TargetPayloadSize               = 512
)

type Message struct {
	Type      MessageType `json:"type"`
	ID        string      `json:"id"`
	Timestamp string      `json:"ts"`
	Data      string      `json:"data"`
}

type Generator struct {
	rng *rand.Rand
}

func NewGenerator(seed int64) *Generator {
	return &Generator{
		rng: rand.New(rand.NewSource(seed)),
	}
}

func (g *Generator) messageType() MessageType {
	r := g.rng.Float64()
	switch {
	case r < 0.70:
		return OrderPlaced
	case r < 0.90:
		return PaymentSettled
	default:
		return InventoryAdjusted
	}
}

func (g *Generator) Generate(index int) ([]byte, MessageType) {
	msgType := g.messageType()

	msg := Message{
		Type:      msgType,
		ID:        fmt.Sprintf("%s-%d", msgType, index),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	// Marshal without padding to measure overhead
	skeleton, _ := json.Marshal(msg)
	overhead := len(skeleton)

	// Pad to reach target size
	padLen := TargetPayloadSize - overhead
	if padLen < 0 {
		padLen = 0
	}
	msg.Data = strings.Repeat("x", padLen)

	data, _ := json.Marshal(msg)
	return data, msgType
}
