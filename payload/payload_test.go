package payload

import (
	"testing"
)

func TestGenerateDeterministic(t *testing.T) {
	g1 := NewGenerator(42)
	g2 := NewGenerator(42)

	for i := 0; i < 100; i++ {
		_, typ1 := g1.Generate(i)
		_, typ2 := g2.Generate(i)
		if typ1 != typ2 {
			t.Fatalf("index %d: types differ with same seed: %s vs %s", i, typ1, typ2)
		}
	}
}

func TestGenerateSize(t *testing.T) {
	g := NewGenerator(42)
	for i := 0; i < 1000; i++ {
		data, _ := g.Generate(i)
		size := len(data)
		// Allow +/- 10% tolerance around 512
		if size < 460 || size > 564 {
			t.Fatalf("index %d: payload size %d outside acceptable range [460, 564]", i, size)
		}
	}
}

func TestGenerateDistribution(t *testing.T) {
	g := NewGenerator(42)
	counts := map[MessageType]int{}
	total := 10000

	for i := 0; i < total; i++ {
		_, typ := g.Generate(i)
		counts[typ]++
	}

	orderPct := float64(counts[OrderPlaced]) / float64(total)
	paymentPct := float64(counts[PaymentSettled]) / float64(total)
	inventoryPct := float64(counts[InventoryAdjusted]) / float64(total)

	// Allow 3% tolerance
	if orderPct < 0.67 || orderPct > 0.73 {
		t.Errorf("OrderPlaced: expected ~70%%, got %.1f%%", orderPct*100)
	}
	if paymentPct < 0.17 || paymentPct > 0.23 {
		t.Errorf("PaymentSettled: expected ~20%%, got %.1f%%", paymentPct*100)
	}
	if inventoryPct < 0.07 || inventoryPct > 0.13 {
		t.Errorf("InventoryAdjusted: expected ~10%%, got %.1f%%", inventoryPct*100)
	}
}
