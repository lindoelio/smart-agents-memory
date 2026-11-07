package memory

import (
	"regexp"
	"strings"
)

// The corpus is written in English by default: it keeps the entries native
// for the models, and it is the user's English reading practice. A body in
// another language is only right when the user asked for it, or when the
// entry quotes something that must stay verbatim.
//
// The check only warns. It never blocks a write.
var enStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true,
	"this": true, "from": true, "when": true, "not": true, "are": true,
	"you": true, "your": true, "but": true, "has": true, "have": true,
	"was": true, "were": true, "will": true, "can": true, "rule": true,
	"why": true, "use": true, "must": true, "only": true, "does": true,
	"did": true, "its": true, "into": true, "than": true, "then": true,
}

var ptMarkers = map[string]bool{
	"não": true, "que": true, "para": true, "com": true, "como": true,
	"onde": true, "quando": true, "deve": true, "usar": true,
	"portanto": true, "porque": true, "corpo": true, "regra": true,
	"memória": true, "projeto": true, "razão": true, "aplicar": true,
	"apenas": true, "então": true, "já": true, "está": true, "são": true,
	"isso": true, "aqui": true,
}

var wordPattern = regexp.MustCompile(`[A-Za-zÀ-ÿ]+`)

// IsEnglish reports whether the text reads as English.
//
// It counts English stopwords against Portuguese markers over the alphabetic
// words. Short text is left alone: there is nothing to judge.
func IsEnglish(text string) bool {
	words := wordPattern.FindAllString(strings.ToLower(text), -1)
	if len(words) < 8 {
		return true
	}
	en, pt := 0, 0
	for _, w := range words {
		if enStopwords[w] {
			en++
		}
		if ptMarkers[w] {
			pt++
		}
	}
	return en >= pt
}
