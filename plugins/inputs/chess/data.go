package chess

// this file contains the data structures used for creating and holding
// the unmarshalled json data returned from the requests of the main
// chess.go file.

type ResponseLeaderboards struct {
	PlayerID int    `json:"player_id"`
	Username string `json:"username"`
	Rank     int    `json:"rank"`
	Score    int    `json:"score"`
}

type Leaderboards struct {
	Daily []ResponseLeaderboards `json:"daily"`
}

type ResponseStreamerData struct {
	Username  string `json:"username"`
	Avatar    string `json:"avatar"`
	TwitchUrl string `json:"twitch_url"`
	Url       string `json:"url"`
}

type Streamers struct {
	Data []ResponseStreamerData `json:"streamers"`
}
