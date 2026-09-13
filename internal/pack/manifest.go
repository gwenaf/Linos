package pack

// SupportedVersion is the newest manifest version this build can read.
const SupportedVersion = 1

type Manifest struct {
	Version  int     `json:"version"`
	Title    string  `json:"title"`
	Author   string  `json:"author"`
	Language string  `json:"language"`
	Cover    string  `json:"cover"`
	Game     Game    `json:"game"`
	Rules    *Rules  `json:"rules"`
	Themes   []Theme `json:"themes"`
	Tracks   []Track `json:"tracks"`
	Rounds   []Round `json:"rounds"`
}

type Game struct {
	Control  Control  `json:"control"`
	Players  Players  `json:"players"`
	End      End      `json:"end"`
	Tiebreak Tiebreak `json:"tiebreak"`
}

type Control struct {
	Allowed []string `json:"allowed"`
	Default string   `json:"default"`
}

type Players struct {
	Teams    string `json:"teams"`
	MinTeams int    `json:"minTeams"`
	MaxTeams int    `json:"maxTeams"`
}

type End struct {
	Type   string `json:"type"`
	Target int    `json:"target"`
}

type Tiebreak struct {
	Type   string   `json:"type"`
	Themes []string `json:"themes"`
}

// Rules fields are pointers so a round can override only what it sets.
type Rules struct {
	Duration *float64     `json:"duration"`
	Answer   *AnswerRules `json:"answer"`
	Owner    *OwnerRules  `json:"owner"`
	Scoring  *Scoring     `json:"scoring"`
	Jokers   *Jokers      `json:"jokers"`
}

type AnswerRules struct {
	Mode         *string  `json:"mode"`
	Via          *string  `json:"via"`
	AnswerTime   *float64 `json:"answerTime"`
	PauseOnBuzz  *bool    `json:"pauseOnBuzz"`
	Attempts     *int     `json:"attempts"`
	WrongLockout *float64 `json:"wrongLockout"`
	Rebound      *string  `json:"rebound"`
	Fuzziness    *float64 `json:"fuzziness"`
}

type OwnerRules struct {
	HeadStart   *float64 `json:"headStart"`
	Exclusive   *bool    `json:"exclusive"`
	OthersBonus *int     `json:"othersBonus"`
}

type Scoring struct {
	Type         *string `json:"type"`
	Max          *int    `json:"max"`
	Min          *int    `json:"min"`
	Ranks        []int   `json:"ranks"`
	WrongPenalty *int    `json:"wrongPenalty"`
	ReboundBonus *int    `json:"reboundBonus"`
}

type Jokers struct {
	Double *int `json:"double"`
}

type Theme struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Track struct {
	ID           string   `json:"id"`
	Themes       []string `json:"themes"`
	Media        string   `json:"media"`
	Start        float64  `json:"start"`
	Duration     float64  `json:"duration"`
	PlaybackRate float64  `json:"playbackRate"`
	Reveal       []Reveal `json:"reveal"`
	Guesses      []Guess  `json:"guesses"`
	Hints        []Hint   `json:"hints"`
}

type Reveal struct {
	At    float64  `json:"at"`
	Audio *bool    `json:"audio"`
	Video *bool    `json:"video"`
	Blur  *float64 `json:"blur"`
}

type Guess struct {
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Answers []string `json:"answers"`
	// Answer is a string for "choice" guesses and a number for "number" guesses.
	Answer    any      `json:"answer"`
	Choices   []string `json:"choices"`
	ChoicesAt float64  `json:"choicesAt"`
	Tolerance float64  `json:"tolerance"`
	Scoring   *Scoring `json:"scoring"`
}

type Hint struct {
	At   float64 `json:"at"`
	Text string  `json:"text"`
}

type Round struct {
	Name      string   `json:"name"`
	Selection string   `json:"selection"`
	Tracks    []string `json:"tracks"`
	Themes    []string `json:"themes"`
	Picker    string   `json:"picker"`
	Count     int      `json:"count"`
	Eliminate int      `json:"eliminate"`
	Rules     *Rules   `json:"rules"`
}
