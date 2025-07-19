package main

import (
	"fmt"
	"regexp"
	"strconv"
)

type LocationP struct {
	SiteName     string
	LatitudeRaw  string
	LongitudeRaw string

	LatitudeDMS  LatitudeDMS
	LongitudeDMS LongitudeDMS

	LatitudeDMD  LatitudeDMD
	LongitudeDMD LongitudeDMD
}

// DirectionNS = N | S
type DirectionNS int

const (
	DirectionN DirectionNS = iota
	DirectionS
)

func (d DirectionNS) String() string {
	switch d {
	case DirectionN:
		return "N"
	case DirectionS:
		return "S"
	default:
		panic("should never happen")
	}
}

// DirectionEW = E | W
type DirectionEW int

const (
	DirectionE DirectionEW = iota
	DirectionW
)

func (d DirectionEW) String() string {
	switch d {
	case DirectionE:
		return "E"
	case DirectionW:
		return "W"
	default:
		panic("should never happen")
	}
}

type LatitudeDMD struct {
	Degrees   int64
	Minutes   float64
	Direction DirectionNS
}

func (d DirectionNS) sign() float64 {
	switch d {
	case DirectionS:
		return -1
	case DirectionN:
		return 1
	default:
		panic("shoud never happen")
	}
}

func (d DirectionEW) sign() float64 {
	switch d {
	case DirectionW:
		return -1
	case DirectionE:
		return 1
	default:
		panic("shoud never happen")
	}
}

// We see some problematic data; let's normalise it instead of throwing an error.
//
// "Site Name: Bahía South, isla Doumer, archipiélago de Palmer - Lat: 64º 60´ 00´´ S - Long: 63º 35´ 00´´ W"

// NormalizeLatitudeDMD ensures minutes are less than 60 by rolling over to degrees.
func (lat *LatitudeDMD) Normalize() {
	if lat.Minutes >= 60 {
		extraDegrees := int64(lat.Minutes) / 60
		lat.Degrees += extraDegrees
		lat.Minutes = lat.Minutes - float64(extraDegrees*60)
	}
}

// NormalizeLatitudeDMS ensures minutes and seconds are less than 60 by rolling over.
func (lat *LatitudeDMS) Normalize() {
	if lat.Seconds >= 60 {
		extraMinutes := int64(lat.Seconds) / 60
		lat.Seconds = lat.Seconds - float64(extraMinutes*60)
		lat.Minutes += extraMinutes
	}
	if lat.Minutes >= 60 {
		extraDegrees := lat.Minutes / 60
		lat.Degrees += extraDegrees
		lat.Minutes = lat.Minutes % 60
	}
}

// Similarly, add Normalize methods for LongitudeDMD and LongitudeDMS.
func (lon *LongitudeDMD) Normalize() {
	if lon.Minutes >= 60 {
		extraDegrees := int64(lon.Minutes) / 60
		lon.Degrees += extraDegrees
		lon.Minutes = lon.Minutes - float64(extraDegrees*60)
	}
}

func (lon *LongitudeDMS) Normalize() {
	if lon.Seconds >= 60 {
		extraMinutes := int64(lon.Seconds) / 60
		lon.Seconds = lon.Seconds - float64(extraMinutes*60)
		lon.Minutes += extraMinutes
	}
	if lon.Minutes >= 60 {
		extraDegrees := lon.Minutes / 60
		lon.Degrees += extraDegrees
		lon.Minutes = lon.Minutes % 60
	}
}

// Conversion methods with normalization.
func (lat LatitudeDMD) ToDecimalDegrees() float64 {
	normalized := lat // Create a copy to avoid mutating the original
	normalized.Normalize()
	dd := float64(normalized.Degrees) + (normalized.Minutes / 60)
	return dd * normalized.Direction.sign()
}

func (lat LatitudeDMS) ToDecimalDegrees() float64 {
	normalized := lat
	normalized.Normalize()
	dd := float64(normalized.Degrees) + (float64(normalized.Minutes) / 60) + (normalized.Seconds / 3600)
	return dd * normalized.Direction.sign()
}

func (lon LongitudeDMD) ToDecimalDegrees() float64 {
	normalized := lon
	normalized.Normalize()
	dd := float64(normalized.Degrees) + (normalized.Minutes / 60)
	return dd * normalized.Direction.sign()
}

func (lon LongitudeDMS) ToDecimalDegrees() float64 {
	normalized := lon
	normalized.Normalize()
	dd := float64(normalized.Degrees) + (float64(normalized.Minutes) / 60) + (normalized.Seconds / 3600)
	return dd * normalized.Direction.sign()
}

type LongitudeDMD struct {
	Degrees   int64
	Minutes   float64
	Direction DirectionEW
}

type LatitudeDMS struct {
	Degrees   int64
	Minutes   int64
	Seconds   float64
	Direction DirectionNS
}

type LongitudeDMS struct {
	Degrees   int64
	Minutes   int64
	Seconds   float64
	Direction DirectionEW
}

func ParseLatitudeDMS(x string) (LatitudeDMS, error) {
	latPattern := `<span><strong>Lat</strong>: </span>(\d+)[°º]\s*(\d+)´\s*(-?\d*\.?\d+)´´\s*([NS]).*`

	latRegex := regexp.MustCompile(latPattern)

	latMatches := latRegex.FindStringSubmatch(x)

	if len(latMatches) > 0 {
		degrees, err := strconv.ParseInt(latMatches[1], 10, 64)
		if err != nil {
			return LatitudeDMS{}, fmt.Errorf("bad degrees %s in %s", latMatches[1], x)
		}

		minutes, err := strconv.ParseInt(latMatches[2], 10, 64)
		if err != nil {
			return LatitudeDMS{}, fmt.Errorf("bad minutes %s in %s", latMatches[2], x)
		}

		seconds, err := strconv.ParseFloat(latMatches[3], 64)
		if err != nil {
			return LatitudeDMS{}, fmt.Errorf("bad seconds %s in %s", latMatches[3], x)
		}

		ixDirection := len(latMatches) - 1

		var direction DirectionNS
		switch latMatches[ixDirection] {
		case "N":
			direction = DirectionN
		case "S":
			direction = DirectionS
		default:
			return LatitudeDMS{}, fmt.Errorf("bad direction %s in %s", latMatches[ixDirection], x)
		}

		lat := LatitudeDMS{
			Degrees:   degrees,
			Minutes:   minutes,
			Seconds:   seconds,
			Direction: direction,
		}

		return lat, nil
	}

	return LatitudeDMS{}, fmt.Errorf("bad LatitudeDMS: %s", x)
}

func ParseLongitudeDMS(x string) (LongitudeDMS, error) {
	lonPattern := `<span><strong>Long</strong>: </span>(\d+)[°º]\s*(\d+)´\s*(-?\d*\.?\d+)´´\s*([EW]).*`

	lonRegex := regexp.MustCompile(lonPattern)

	lonMatches := lonRegex.FindStringSubmatch(x)

	if len(lonMatches) > 0 {
		degrees, err := strconv.ParseInt(lonMatches[1], 10, 64)
		if err != nil {
			return LongitudeDMS{}, fmt.Errorf("bad degrees %s in %s", lonMatches[1], x)
		}

		minutes, err := strconv.ParseInt(lonMatches[2], 10, 64)
		if err != nil {
			return LongitudeDMS{}, fmt.Errorf("bad minutes %s in %s", lonMatches[2], x)
		}

		seconds, err := strconv.ParseFloat(lonMatches[3], 64)
		if err != nil {
			return LongitudeDMS{}, fmt.Errorf("bad seconds %s in %s", lonMatches[3], x)
		}

		ixDirection := len(lonMatches) - 1

		var direction DirectionEW
		switch lonMatches[ixDirection] {
		case "E":
			direction = DirectionE
		case "W":
			direction = DirectionW
		default:
			return LongitudeDMS{}, fmt.Errorf("bad direction %s in %s", lonMatches[ixDirection], x)
		}

		lon := LongitudeDMS{
			Degrees:   degrees,
			Minutes:   minutes,
			Seconds:   seconds,
			Direction: direction,
		}

		return lon, nil
	}

	return LongitudeDMS{}, fmt.Errorf("bad LongitudeDMS: %s", x)

}

func ParseLatitudeDMD(x string) (LatitudeDMD, error) {
	latPattern := `<span><strong>Lat</strong>: </span>(\d+)[°º](\d+)´([NS]).*`

	latRegex := regexp.MustCompile(latPattern)

	latMatches := latRegex.FindStringSubmatch(x)

	if len(latMatches) > 0 {
		// latMatches[0] is the full match
		// latMatches[1] is degrees
		// latMatches[2] is minutes
		// latMatches[3] is direction (N/S)

		degrees, err := strconv.ParseInt(latMatches[1], 10, 64)
		if err != nil {
			return LatitudeDMD{}, fmt.Errorf("bad degrees %s in %s", latMatches[1], x)
		}

		minutes, err := strconv.ParseFloat(latMatches[2], 64)
		if err != nil {
			return LatitudeDMD{}, fmt.Errorf("bad minutes %s in %s", latMatches[2], x)
		}

		var direction DirectionNS
		switch latMatches[3] {
		case "N":
			direction = DirectionN
		case "S":
			direction = DirectionS
		default:
			return LatitudeDMD{}, fmt.Errorf("bad direction %s in %s", latMatches[3], x)
		}

		lat := LatitudeDMD{
			Degrees:   degrees,
			Minutes:   minutes,
			Direction: direction,
		}

		return lat, nil
	}

	return LatitudeDMD{}, fmt.Errorf("bad DMD string %s", x)
}

func ParseLongitudeDMD(x string) (LongitudeDMD, error) {
	lonPattern := `<span><strong>Long</strong>: </span>(\d+)[°º](\d+)´([EW]).*`

	lonRegex := regexp.MustCompile(lonPattern)

	lonMatches := lonRegex.FindStringSubmatch(x)

	if len(lonMatches) > 0 {
		// lonMatches[0] is the full match
		// lonMatches[1] is degrees
		// lonMatches[2] is minutes
		// lonMatches[3] is direction (E/W)

		degrees, err := strconv.ParseInt(lonMatches[1], 10, 64)
		if err != nil {
			return LongitudeDMD{}, fmt.Errorf("bad degrees %s in %s", lonMatches[1], x)
		}

		minutes, err := strconv.ParseFloat(lonMatches[2], 64)
		if err != nil {
			return LongitudeDMD{}, fmt.Errorf("bad minutes %s in %s", lonMatches[2], x)
		}

		var direction DirectionEW
		switch lonMatches[3] {
		case "E":
			direction = DirectionE
		case "W":
			direction = DirectionW
		default:
			return LongitudeDMD{}, fmt.Errorf("bad direction %s in %s", lonMatches[3], x)
		}

		lon := LongitudeDMD{
			Degrees:   degrees,
			Minutes:   minutes,
			Direction: direction,
		}

		return lon, nil
	}

	return LongitudeDMD{}, fmt.Errorf("bad DMD string %s", x)
}
