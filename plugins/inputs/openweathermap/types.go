package openweathermap

type weatherEntry struct {
	Dt     int64 `json:"dt"`
	Clouds struct {
		All int64 `json:"all"`
	} `json:"clouds"`
	Main struct {
		Humidity int64   `json:"humidity"`
		Pressure float64 `json:"pressure"`
		Temp     float64 `json:"temp"`
		Feels    float64 `json:"feels_like"`
	} `json:"main"`
	Rain struct {
		Rain1 float64 `json:"1h"`
		Rain3 float64 `json:"3h"`
	} `json:"rain"`
	Snow struct {
		Snow1 float64 `json:"1h"`
		Snow3 float64 `json:"3h"`
	} `json:"snow"`
	Sys struct {
		Country string `json:"country"`
		Sunrise int64  `json:"sunrise"`
		Sunset  int64  `json:"sunset"`
	} `json:"sys"`
	Wind struct {
		Deg   float64 `json:"deg"`
		Speed float64 `json:"speed"`
	} `json:"wind"`
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Coord struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"coord"`
	Visibility int64 `json:"visibility"`
	Weather    []struct {
		ID          int64  `json:"id"`
		Main        string `json:"main"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	} `json:"weather"`
}

func (e weatherEntry) snow() float64 {
	if e.Snow.Snow1 > 0 {
		return e.Snow.Snow1
	}
	return e.Snow.Snow3
}

func (e weatherEntry) rain() float64 {
	if e.Rain.Rain1 > 0 {
		return e.Rain.Rain1
	}
	return e.Rain.Rain3
}

type status struct {
	City struct {
		Coord struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"coord"`
		Country string `json:"country"`
		ID      int64  `json:"id"`
		Name    string `json:"name"`
	} `json:"city"`
	List []weatherEntry `json:"list"`
}
