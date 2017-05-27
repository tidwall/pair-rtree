package cities

import (
	"strconv"

	"github.com/tidwall/geobin"
	"github.com/tidwall/pair"
)

func Pairs() []pair.Pair {
	pairs := make([]pair.Pair, 0, len(Cities))
	var key []byte
	for _, city := range Cities {
		key = strconv.AppendInt(key[:0], int64(city.ID), 10)
		item := pair.New(key, geobin.Make3DPoint(city.Longitude, city.Latitude, city.Altitude).Binary())
		pairs = append(pairs, item)
	}
	return pairs
}
