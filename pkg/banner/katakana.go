package banner

import (
	"strings"

	figure "github.com/common-nighthawk/go-figure"
)

// kanaLetters maps katakana to the letters of the FIGlet katakana font.
// The mapping is listed in the header of katakana.flf by Vinney Thai.
var kanaLetters = map[rune]byte{
	'ア': 'A', 'イ': 'B', 'ウ': 'C', 'エ': 'D', 'オ': 'E',
	'カ': 'F', 'キ': 'G', 'ク': 'H', 'ケ': 'I', 'コ': 'J',
	'サ': 'K', 'シ': 'L', 'ス': 'M', 'セ': 'N', 'ソ': 'O',
	'タ': 'P', 'チ': 'Q', 'ツ': 'R', 'テ': 'S', 'ト': 'T',
	'ナ': 'U', 'ニ': 'V', 'ヌ': 'W', 'ネ': 'X', 'ノ': 'Y',
	'ハ': 'Z', 'ヒ': 'a', 'フ': 'b', 'ヘ': 'c', 'ホ': 'd',
	'マ': 'e', 'ミ': 'f', 'ム': 'g', 'メ': 'h', 'モ': 'i',
	'ヤ': 'j', 'ユ': 'k', 'ヨ': 'm',
	'ラ': 'n', 'リ': 'o', 'ル': 'p', 'レ': 'q', 'ロ': 'r',
	'ワ': 's', 'ヰ': 't', 'ヱ': 'l', 'ヲ': 'u', 'ン': 'v',
}

var kanaBase = map[rune]rune{
	'ァ': 'ア', 'ィ': 'イ', 'ゥ': 'ウ', 'ェ': 'エ', 'ォ': 'オ',
	'ッ': 'ツ', 'ャ': 'ヤ', 'ュ': 'ユ', 'ョ': 'ヨ', 'ヮ': 'ワ', 'ヵ': 'カ', 'ヶ': 'ケ',
}

var kanaVoiced = map[rune]rune{
	'ガ': 'カ', 'ギ': 'キ', 'グ': 'ク', 'ゲ': 'ケ', 'ゴ': 'コ',
	'ザ': 'サ', 'ジ': 'シ', 'ズ': 'ス', 'ゼ': 'セ', 'ゾ': 'ソ',
	'ダ': 'タ', 'ヂ': 'チ', 'ヅ': 'ツ', 'デ': 'テ', 'ド': 'ト',
	'バ': 'ハ', 'ビ': 'ヒ', 'ブ': 'フ', 'ベ': 'ヘ', 'ボ': 'ホ',
	'ヴ': 'ウ', 'ヷ': 'ワ', 'ヺ': 'ヲ',
}

var kanaSemiVoiced = map[rune]rune{
	'パ': 'ハ', 'ピ': 'ヒ', 'プ': 'フ', 'ペ': 'ヘ', 'ポ': 'ホ',
}

var (
	dakuten    = []string{"  # #", "   # #"}
	handakuten = []string{"  ## ", " #  #", "  ## "}
	chouon     = []string{"", "", "", "#########", "", "", ""}
)

const letterGap = 3

// GenerateKatakana returns a banner for katakana text using the FIGlet
// katakana font. Runes it cannot draw are skipped.
func GenerateKatakana(text string) string {
	var rows []string
	for _, r := range text {
		glyph := kanaGlyph(r)
		if glyph == nil {
			continue
		}
		if rows == nil {
			rows = make([]string, len(glyph))
		}
		for i := range rows {
			if i < len(glyph) {
				rows[i] += glyph[i] + strings.Repeat(" ", letterGap)
			}
		}
	}
	return "\n" + TrimRightLines(strings.Join(rows, "\n")) + "\n"
}

func kanaGlyph(r rune) []string {
	if r == 'ー' {
		return pad(chouon)
	}
	var mark []string
	if b, ok := kanaBase[r]; ok {
		r = b
	} else if b, ok := kanaVoiced[r]; ok {
		r, mark = b, dakuten
	} else if b, ok := kanaSemiVoiced[r]; ok {
		r, mark = b, handakuten
	}
	letter, ok := kanaLetters[r]
	if !ok {
		return nil
	}
	glyph := pad(figure.NewFigure(string(letter), "katakana", true).Slicify())
	if mark != nil {
		glyph = pad(append([]string(nil), glyph...))
		for i := range glyph {
			if i < len(mark) {
				glyph[i] += mark[i]
			}
		}
		glyph = pad(glyph)
	}
	return glyph
}

// pad makes every row as wide as the widest row.
func pad(rows []string) []string {
	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row + strings.Repeat(" ", width-len(row))
	}
	return out
}
