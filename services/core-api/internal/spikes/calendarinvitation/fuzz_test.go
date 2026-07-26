package calendarinvitation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzFoldContentLine(f *testing.F) {
	for _, seed := range []string{
		"SUMMARY:TutorHub",
		"DESCRIPTION:" + strings.Repeat("a", 160),
		"SUMMARY:Lịch học trực tuyến – TutorHub",
		"ATTENDEE;CN=\"Nguyễn Bá Sáng\":mailto:student@example.test",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		input = strings.ToValidUTF8(input, "\uFFFD")
		if len(input) > 8*1024 || strings.ContainsAny(input, "\r\n") {
			t.Skip()
		}

		folded := foldContentLine(input)
		if !utf8.ValidString(folded) {
			t.Fatal("folded content line is not valid UTF-8")
		}

		physicalLines := strings.Split(folded, "\r\n")
		var unfolded strings.Builder
		for index, line := range physicalLines {
			if len(line) > 75 {
				t.Fatalf("physical line %d has %d octets", index, len(line))
			}
			if index != 0 {
				if !strings.HasPrefix(line, " ") {
					t.Fatalf("continuation line %d does not begin with one space", index)
				}
				line = strings.TrimPrefix(line, " ")
			}
			unfolded.WriteString(line)
		}
		if unfolded.String() != input {
			t.Fatal("folding did not preserve the logical content line")
		}
	})
}
