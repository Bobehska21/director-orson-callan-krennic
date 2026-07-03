package ai

import "strings"

// price is USD per 1M tokens (input, output). Approximate list prices used only
// for local budget accounting/telemetry — not billing-authoritative.
type price struct{ in, out float64 }

var priceTable = map[string]price{
	"claude-haiku":   {0.80, 4.00},
	"claude-sonnet":  {3.00, 15.00},
	"claude-opus":    {15.00, 75.00},
	"gemini-flash":   {0.075, 0.30},
	"gemini-pro":     {1.25, 5.00},
}

// costUSD estimates spend for a model given token counts.
func costUSD(modelName string, in, out int) float64 {
	p := lookupPrice(modelName)
	return (float64(in)/1e6)*p.in + (float64(out)/1e6)*p.out
}

func lookupPrice(modelName string) price {
	m := strings.ToLower(modelName)
	switch {
	case strings.Contains(m, "haiku"):
		return priceTable["claude-haiku"]
	case strings.Contains(m, "sonnet"):
		return priceTable["claude-sonnet"]
	case strings.Contains(m, "opus"):
		return priceTable["claude-opus"]
	case strings.Contains(m, "flash"):
		return priceTable["gemini-flash"]
	case strings.Contains(m, "gemini"):
		return priceTable["gemini-pro"]
	default:
		return price{}
	}
}
