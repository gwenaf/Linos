package pack

// SupportedVersion is the newest manifest version this build can read.
const SupportedVersion = 1

type Manifest struct {
	Version  int     `json:"version"`
	Title    string  `json:"title"`
	Author   string  `json:"author,omitempty"`
	Language string  `json:"language,omitempty"`
	Cover    string  `json:"cover,omitempty"`
	Game     Game    `json:"game,omitzero"`
	Rules    *Rules  `json:"rules,omitempty"`
	Themes   []Theme `json:"themes,omitempty"`
	Tracks   []Track `json:"tracks"`
	Rounds   []Round `json:"rounds,omitempty"`
}

type Game struct {
	Control  Control  `json:"control,omitzero"`
	Players  Players  `json:"players,omitzero"`
	End      End      `json:"end,omitzero"`
	Tiebreak Tiebreak `json:"tiebreak,omitzero"`
}

type Control struct {
	Allowed []string `json:"allowed,omitempty"`
	Default string   `json:"default,omitempty"`
}

type Players struct {
	Teams    string `json:"teams,omitempty"`
	MinTeams int    `json:"minTeams,omitempty"`
	MaxTeams int    `json:"maxTeams,omitempty"`
}

type End struct {
	Type   string `json:"type,omitempty"`
	Target int    `json:"target,omitempty"`
}

type Tiebreak struct {
	Type   string   `json:"type,omitempty"`
	Themes []string `json:"themes,omitempty"`
}

// Rules fields are pointers so a round can override only what it sets.
type Rules struct {
	Duration *float64     `json:"duration,omitempty"`
	Answer   *AnswerRules `json:"answer,omitempty"`
	Owner    *OwnerRules  `json:"owner,omitempty"`
	Scoring  *Scoring     `json:"scoring,omitempty"`
	Jokers   *Jokers      `json:"jokers,omitempty"`
}

type AnswerRules struct {
	Mode         *string  `json:"mode,omitempty"`
	Via          *string  `json:"via,omitempty"`
	AnswerTime   *float64 `json:"answerTime,omitempty"`
	PauseOnBuzz  *bool    `json:"pauseOnBuzz,omitempty"`
	Attempts     *int     `json:"attempts,omitempty"`
	WrongLockout *float64 `json:"wrongLockout,omitempty"`
	Rebound      *string  `json:"rebound,omitempty"`
	Fuzziness    *float64 `json:"fuzziness,omitempty"`
	// Partial is how the gamemaster's validation of an answer missing some guesses counts: "each" pays
	// each guess found, "all" counts it wrong.
	Partial *string `json:"partial,omitempty"`
	// IncompletePenalty is taken when a validated answer leaves guesses unfound (partial "each").
	IncompletePenalty *int `json:"incompletePenalty,omitempty"`
}

type OwnerRules struct {
	HeadStart   *float64 `json:"headStart,omitempty"`
	Exclusive   *bool    `json:"exclusive,omitempty"`
	OthersBonus *int     `json:"othersBonus,omitempty"`
	// OthersMultiplier multiplies the points of a team finding on another team's theme.
	OthersMultiplier *int `json:"othersMultiplier,omitempty"`
	// StolenPenalty is taken from the owner when another team finds; with StolenLosesPoints the owner
	// also loses what that team won.
	StolenPenalty     *int  `json:"stolenPenalty,omitempty"`
	StolenLosesPoints *bool `json:"stolenLosesPoints,omitempty"`
}

type Scoring struct {
	Type         *string `json:"type,omitempty"`
	Max          *int    `json:"max,omitempty"`
	Min          *int    `json:"min,omitempty"`
	Ranks        []int   `json:"ranks,omitempty"`
	WrongPenalty *int    `json:"wrongPenalty,omitempty"`
	ReboundBonus *int    `json:"reboundBonus,omitempty"`
}

type Jokers struct {
	Double *int `json:"double,omitempty"`
}

type Theme struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Track struct {
	ID           string   `json:"id"`
	Themes       []string `json:"themes,omitempty"`
	Media        string   `json:"media"`
	Start        float64  `json:"start,omitempty"`
	Duration     float64  `json:"duration,omitempty"`
	PlaybackRate float64  `json:"playbackRate,omitempty"`
	// Outro is the time played after the track ends, fading out; 0 stops the media.
	Outro float64 `json:"outro,omitempty"`
	// OutroMedia, when set, plays for the outro instead of the rest of Media, from OutroStart and at
	// normal speed: the bass line alone as question, the whole song as answer.
	OutroMedia string   `json:"outroMedia,omitempty"`
	OutroStart float64  `json:"outroStart,omitempty"`
	Reveal     []Reveal `json:"reveal,omitempty"`
	Guesses    []Guess  `json:"guesses,omitempty"`
	Hints      []Hint   `json:"hints,omitempty"`
}

// Reveal is a step of the effects /host applies live: numeric effects move linearly
// to the next step setting them, the others switch at At.
type Reveal struct {
	At        float64  `json:"at,omitempty"`
	Audio     *bool    `json:"audio,omitempty"`
	Video     *bool    `json:"video,omitempty"`
	Blur      *float64 `json:"blur,omitempty"`
	Pixelate  *float64 `json:"pixelate,omitempty"`
	Grayscale *float64 `json:"grayscale,omitempty"`
	// Image is a pack picture shown instead of the video frames; "" removes it.
	Image *string `json:"image,omitempty"`
}

type Guess struct {
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Answers []string `json:"answers,omitempty"`
	// Answer is a string for "choice" guesses and a number for "number" guesses.
	Answer    any      `json:"answer,omitempty"`
	Choices   []string `json:"choices,omitempty"`
	ChoicesAt float64  `json:"choicesAt,omitempty"`
	Tolerance float64  `json:"tolerance,omitempty"`
	Scoring   *Scoring `json:"scoring,omitempty"`
}

type Hint struct {
	At   float64 `json:"at,omitempty"`
	Text string  `json:"text,omitempty"`
}

type Round struct {
	Name      string   `json:"name"`
	Selection string   `json:"selection"`
	Tracks    []string `json:"tracks,omitempty"`
	Themes    []string `json:"themes,omitempty"`
	Picker    string   `json:"picker,omitempty"`
	// PickScope is what a theme pick plays: "track", one drawn track, or "theme", all its tracks left.
	PickScope string `json:"pickScope,omitempty"`
	Count     int    `json:"count,omitempty"`
	Eliminate int    `json:"eliminate,omitempty"`
	Rules     *Rules `json:"rules,omitempty"`
}

// Files lists every file the manifest uses: the cover, the media of the tracks and their outros, and
// the reveal pictures. The game serves these and nothing else.
func (m Manifest) Files() []string {
	files := referencedImages(m)
	for _, t := range m.Tracks {
		files = append(files, t.Media)
		if t.OutroMedia != "" {
			files = append(files, t.OutroMedia)
		}
	}
	return files
}
