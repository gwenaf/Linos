package pack

import (
	"strings"
	"testing"
)

func firstTrack(m map[string]any) map[string]any {
	return m["tracks"].([]any)[0].(map[string]any)
}

func firstGuess(m map[string]any) map[string]any {
	return firstTrack(m)["guesses"].([]any)[0].(map[string]any)
}

type obj = map[string]any
type list = []any

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m obj)
		want   string
	}{
		{"version 0", func(m obj) { m["version"] = 0 }, "version 0 is not supported"},
		{"future version", func(m obj) { m["version"] = 2 }, "version 2 is not supported"},
		{"title", func(m obj) { m["title"] = " " }, "title is required"},
		{"cover missing", func(m obj) { m["cover"] = "cover.jpg" }, `cover "cover.jpg" not found`},
		{"cover path", func(m obj) { m["cover"] = "../cover.jpg" }, "not a valid relative path"},
		{"outro media missing", func(m obj) { firstTrack(m)["outroMedia"], firstTrack(m)["outro"] = "full.mp3", 5 }, `tracks[0].outroMedia "full.mp3" not found`},
		{"outro media type", func(m obj) { firstTrack(m)["outroMedia"], firstTrack(m)["outro"] = "notes.txt", 5 }, "not a supported media file"},
		{"outro media unheard", func(m obj) { firstTrack(m)["outroMedia"] = "a.mp3" }, "an outroMedia needs an outro duration"},
		{"outro start", func(m obj) { firstTrack(m)["outroStart"] = -1 }, "outroStart cannot be negative"},
		{"media type", func(m obj) { m["cover"] = "page.html" }, `"page.html" is not a supported media file`},
		{"control allowed", func(m obj) { m["game"] = obj{"control": obj{"allowed": list{"robot"}}} }, `game.control.allowed "robot"`},
		{"control default", func(m obj) { m["game"] = obj{"control": obj{"allowed": list{"master"}, "default": "auto"}} }, "game.control.default"},
		{"teams mode", func(m obj) { m["game"] = obj{"players": obj{"teams": "sometimes"}} }, "game.players.teams"},
		{"team bounds", func(m obj) { m["game"] = obj{"players": obj{"minTeams": 4, "maxTeams": 2}} }, "minTeams exceeds maxTeams"},
		{"end type", func(m obj) { m["game"] = obj{"end": obj{"type": "never"}} }, "game.end.type"},
		{"score target", func(m obj) { m["game"] = obj{"end": obj{"type": "score"}} }, "game.end.target"},
		{"tiebreak type", func(m obj) { m["game"] = obj{"tiebreak": obj{"type": "coin"}} }, "game.tiebreak.type"},
		{"tiebreak theme", func(m obj) { m["game"] = obj{"tiebreak": obj{"type": "sudden-death", "themes": list{"nope"}}} }, `game.tiebreak.themes: unknown id "nope"`},
		{"answer mode", func(m obj) { m["rules"] = obj{"answer": obj{"mode": "shout"}} }, "rules.answer.mode"},
		{"answer via", func(m obj) { m["rules"] = obj{"answer": obj{"via": "telepathy"}} }, "rules.answer.via"},
		{"rebound", func(m obj) { m["rules"] = obj{"answer": obj{"rebound": "some"}} }, "rules.answer.rebound"},
		{"fuzziness", func(m obj) { m["rules"] = obj{"answer": obj{"fuzziness": 2}} }, "fuzziness must be between 0 and 1"},
		{"partial", func(m obj) { m["rules"] = obj{"answer": obj{"partial": "some"}} }, "rules.answer.partial"},
		{"incomplete penalty", func(m obj) { m["rules"] = obj{"answer": obj{"incompletePenalty": -1}} }, "incompletePenalty cannot be negative"},
		{"others multiplier", func(m obj) { m["rules"] = obj{"owner": obj{"othersMultiplier": 0}} }, "othersMultiplier must be at least 1"},
		{"stolen penalty", func(m obj) { m["rules"] = obj{"owner": obj{"stolenPenalty": -5}} }, "stolenPenalty cannot be negative"},
		{"pick scope", func(m obj) {
			m["rounds"] = list{obj{"selection": "theme-pick", "themes": list{"fr"}, "count": 1, "pickScope": "all"}}
		}, "rounds[0].pickScope"},
		{"attempts", func(m obj) { m["rules"] = obj{"answer": obj{"attempts": 0}} }, "attempts must be at least 1"},
		{"lockout", func(m obj) { m["rules"] = obj{"answer": obj{"wrongLockout": -1}} }, "wrongLockout cannot be negative"},
		{"scoring type", func(m obj) { m["rules"] = obj{"scoring": obj{"type": "lottery"}} }, "rules.scoring.type"},
		{"scoring bounds", func(m obj) { m["rules"] = obj{"scoring": obj{"max": 10, "min": 20}} }, "rules.scoring.min exceeds max"},
		{"theme id", func(m obj) { m["themes"] = append(m["themes"].(list), obj{"name": "x"}) }, "themes[1].id is required"},
		{"theme duplicate", func(m obj) { m["themes"] = append(m["themes"].(list), obj{"id": "fr"}) }, `themes[1].id "fr" is duplicated`},
		{"no tracks", func(m obj) { m["tracks"] = list{} }, "at least one track is required"},
		{"track id", func(m obj) { delete(firstTrack(m), "id") }, "tracks[0].id is required"},
		{"track duplicate", func(m obj) { m["tracks"] = append(m["tracks"].(list), firstTrack(m)) }, `tracks[1].id "t1" is duplicated`},
		{"track theme", func(m obj) { firstTrack(m)["themes"] = list{"nope"} }, `tracks[0].themes: unknown id "nope"`},
		{"media path", func(m obj) { firstTrack(m)["media"] = `media\a.mp3` }, "not a valid relative path"},
		{"media missing", func(m obj) { firstTrack(m)["media"] = "b.mp3" }, `tracks[0].media "b.mp3" not found`},
		{"negative start", func(m obj) { firstTrack(m)["start"] = -1 }, "cannot be negative"},
		{"reveal negative", func(m obj) { firstTrack(m)["reveal"] = list{obj{"at": 0, "pixelate": -2}} }, "reveal[0]: at, blur and pixelate cannot be negative"},
		{"reveal grayscale", func(m obj) { firstTrack(m)["reveal"] = list{obj{"grayscale": 2}} }, "reveal[0].grayscale must be between 0 and 1"},
		{"reveal image", func(m obj) { firstTrack(m)["reveal"] = list{obj{"image": ""}, obj{"image": "cover.jpg"}} }, `reveal[1].image "cover.jpg" not found`},
		{"no guesses", func(m obj) { firstTrack(m)["guesses"] = list{} }, "at least one guess is required"},
		{"guess label", func(m obj) { firstGuess(m)["label"] = "" }, "guesses[0].label is required"},
		{"guess label duplicate", func(m obj) {
			firstTrack(m)["guesses"] = append(firstTrack(m)["guesses"].(list), firstGuess(m))
		}, `guesses[1].label "Titre" is duplicated`},
		{"guess type", func(m obj) { firstGuess(m)["type"] = "essay" }, "guesses[0].type"},
		{"text answers", func(m obj) { delete(firstGuess(m), "answers") }, "a text guess needs answers"},
		{"choice answer", func(m obj) {
			g := firstGuess(m)
			g["type"], g["choices"], g["answer"] = "choice", list{"a", "b"}, "c"
		}, "answer must be one of choices"},
		{"number answer", func(m obj) {
			g := firstGuess(m)
			g["type"], g["answer"] = "number", "1984"
		}, "answer must be a number"},
		{"guess scoring", func(m obj) { firstGuess(m)["scoring"] = obj{"max": 1, "min": 5} }, "guesses[0].scoring.min exceeds max"},
		{"selection", func(m obj) { m["rounds"] = list{obj{"selection": "shuffle"}} }, "rounds[0].selection"},
		{"sequence tracks", func(m obj) { m["rounds"] = list{obj{"selection": "sequence"}} }, "a sequence round needs tracks"},
		{"sequence unknown track", func(m obj) { m["rounds"] = list{obj{"selection": "sequence", "tracks": list{"nope"}}} }, `rounds[0].tracks: unknown id "nope"`},
		{"random themes", func(m obj) { m["rounds"] = list{obj{"selection": "random", "count": 1}} }, "a random round needs themes"},
		{"theme-pick unknown theme", func(m obj) { m["rounds"] = list{obj{"selection": "theme-pick", "themes": list{"nope"}, "count": 1}} }, `rounds[0].themes: unknown id "nope"`},
		{"count", func(m obj) { m["rounds"] = list{obj{"selection": "random", "themes": list{"fr"}}} }, "rounds[0].count must be positive"},
		{"picker", func(m obj) { m["rounds"] = list{obj{"selection": "sequence", "tracks": list{"t1"}, "picker": "vote"}} }, "rounds[0].picker"},
		{"round rules", func(m obj) {
			m["rounds"] = list{obj{"selection": "sequence", "tracks": list{"t1"}, "rules": obj{"answer": obj{"mode": "x"}}}}
		}, "rounds[0].rules.answer.mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := baseManifest()
			tc.mutate(m)
			_, err := Open(writeDir(t, map[string]string{"manifest.json": mustJSON(t, m), "a.mp3": ""}))
			expectErr(t, err, tc.want)
		})
	}
}

// Limits are inclusive: a bound itself is a valid value.
func TestValidationBoundaries(t *testing.T) {
	m := baseManifest()
	m["game"] = obj{"players": obj{"minTeams": 2, "maxTeams": 2}}
	// maxTeams 0 means no maximum, so any minimum is fine.
	m2 := baseManifest()
	m2["game"] = obj{"players": obj{"minTeams": 2}}
	if _, err := Open(writeDir(t, map[string]string{"manifest.json": mustJSON(t, m2), "a.mp3": ""})); err != nil {
		t.Fatalf("minTeams without maxTeams refused: %v", err)
	}
	m["rules"] = obj{
		"answer":  obj{"fuzziness": 0, "wrongLockout": 0},
		"scoring": obj{"min": 5, "max": 5},
	}
	m["rounds"] = list{obj{"selection": "sequence", "tracks": list{"t1"}, "rules": obj{"answer": obj{"fuzziness": 1}}}}
	firstTrack(m)["outroMedia"], firstTrack(m)["outro"], firstTrack(m)["outroStart"] = "a.mp3", 5, 0
	m["rules"].(obj)["owner"] = obj{"othersMultiplier": 1, "stolenPenalty": 0}
	m["rules"].(obj)["answer"].(obj)["incompletePenalty"] = 0
	data := mustJSON(t, m)
	data += strings.Repeat(" ", MaxManifest-len(data)) // exactly the size limit
	p, err := Open(writeDir(t, map[string]string{"manifest.json": data, "a.mp3": ""}))
	if err != nil {
		t.Fatalf("boundary values refused: %v", err)
	}
	p.Close()
}

func TestValidationReportsAllErrors(t *testing.T) {
	m := baseManifest()
	m["title"] = ""
	firstTrack(m)["media"] = "b.mp3"
	_, err := Open(writeDir(t, map[string]string{"manifest.json": mustJSON(t, m)}))
	expectErr(t, err, "title is required")
	expectErr(t, err, `"b.mp3" not found`)
}
