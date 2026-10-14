// Package search ranks library commands against a request typed in plain
// words ("xem cổng đang mở", "show open ports"): most relevant first.
package search

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/thangkes/cmdex/internal/store"
)

// Normalize lowercases s and removes Vietnamese (and other) diacritics, so
// "Cổng" and "cong" match.
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r == 'đ' || r == 'Đ':
			b.WriteRune('d')
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// Words splits normalized text into words of letters and digits.
func Words(s string) []string {
	return strings.FieldsFunc(Normalize(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Result is one ranked command.
type Result struct {
	Command store.Command `json:"command"`
	Score   float64       `json:"score"`
	Percent int           `json:"percent"` // relative to the best match, 1..100
}

type term struct {
	text   string
	phrase bool
	weight float64
}

// field weights: where a match counts most
var weights = []float64{3, 2.5, 2, 1.5, 1.5, 1}

// Rank scores cmds against query. Words also match through syn (normalized
// word or two-word phrase -> alternatives). Commands for platform os (or for
// both) rank higher when os is set. An empty query lists the most used
// commands. At most limit results (0 = all).
func Rank(cmds []store.Command, query string, syn map[string][]string, os string, limit int) []Result {
	words := Words(query)
	var terms []term
	for i, w := range words {
		terms = append(terms, term{text: w, weight: 1})
		if i+1 < len(words) {
			terms = append(terms, term{text: w + " " + words[i+1], phrase: true, weight: 1.4})
		}
	}
	for _, t := range append([]term(nil), terms...) {
		for _, alt := range syn[t.text] {
			alt = Normalize(alt)
			terms = append(terms, term{text: alt, phrase: strings.Contains(alt, " "), weight: t.weight * 0.6})
		}
	}

	var out []Result
	for _, c := range cmds {
		var score float64
		if len(terms) == 0 {
			score = float64(c.Uses) + 1
			if store.IsDevice(os) && c.Platform == os {
				score += 1e6 // on a device, its own commands lead the list
			}
			if os != "" && store.IsDevice(c.Platform) && c.Platform != os {
				score *= 0.5 // and stay behind elsewhere
			}
		} else {
			fields := []string{c.Name, strings.Join(c.Tags, " "), c.Purpose, c.Template, c.Category, c.Notes}
			for i, f := range fields {
				nf := Normalize(f)
				fw := Words(f)
				for _, t := range terms {
					if matches(t, nf, fw) {
						score += weights[i] * t.weight
					}
				}
			}
			if score == 0 {
				continue
			}
			score += math.Min(float64(c.Uses), 20) * 0.03
			switch {
			case os == "":
			case store.IsDevice(os) && c.Platform == os:
				score += 2 // on a device, its own commands first
			case !store.IsDevice(os) && (c.Platform == os || c.Platform == store.Both):
				score += 1.2
			}
			if os != "" && store.IsDevice(c.Platform) && c.Platform != os {
				score *= 0.5 // a device's commands stay behind on a computer or another device
			}
		}
		out = append(out, Result{Command: c, Score: score})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if len(out) > 0 {
		best := out[0].Score
		for i := range out {
			out[i].Percent = int(math.Max(1, math.Round(100*out[i].Score/best)))
		}
	}
	return out
}

// matches: a phrase must appear as is; a word must equal a field word, or
// start one when it is at least three letters long ("cong" finds "cong",
// "port" finds "ports").
func matches(t term, field string, fieldWords []string) bool {
	if t.phrase {
		return strings.Contains(" "+strings.Join(fieldWords, " ")+" ", " "+t.text+" ") || strings.Contains(field, t.text)
	}
	for _, fw := range fieldWords {
		if fw == t.text || (len(t.text) >= 3 && strings.HasPrefix(fw, t.text)) {
			return true
		}
	}
	return false
}
