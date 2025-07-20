package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/carlohamalainen/antarctic-database-go"
)

const emptyLatitude = "<span><strong>Lat</strong>: </span>---  -"
const emptyLongitude = "<span><strong>Long</strong>: </span>---"

func parseTable(table *goquery.Selection) []Report {
	var reports []Report

	for _, tr := range table.Find("tbody tr").EachIter() {
		report := Report{}

		// Get country name
		report.Country = tr.Find("th p").Text()

		// Get links from each column
		for j, td := range tr.Find("td").EachIter() {
			var links []ats.URL
			for _, a := range td.Find("a").EachIter() {
				href, exists := a.Attr("href")
				if exists {
					links = append(links, ats.URL(href))
				}
			}

			switch j {
			case 0:
				report.English = links
			case 1:
				report.Spanish = links
			case 2:
				report.French = links
			case 3:
				report.Russian = links
			default:
				panic(fmt.Errorf("unknown language offset %d", j))
			}
		}

		reports = append(reports, report)
	}

	return reports
}

func parseReports(html []byte) (ReportCollection, error) {

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return ReportCollection{}, err
	}

	reportCollection := ReportCollection{}

	for _, section := range doc.Find(".accordion__item").EachIter() {
		sectionTitle := section.Find(".accordion__item__button__text").Text()
		table := section.Find("table.table__data")

		reports := parseTable(table)

		switch {
		case strings.Contains(sectionTitle, "Annual Information"):
			reportCollection.Annual = reports
		case strings.Contains(sectionTitle, "Pre-season Information"):
			reportCollection.PreSeason = reports
		default:
			panic(fmt.Errorf("section does not appear to be Annual or Pre-season: %s", sectionTitle))
		}
	}

	return reportCollection, nil
}

func parseLocation(content string) (LocationP, error) {
	var loc LocationP

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Site Name") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				siteName := strings.Split(parts[1], "-")[0]
				loc.SiteName = strings.TrimSpace(siteName)
			}
		}
		if strings.Contains(line, "Lat") {
			lat0, err0 := ParseLatitudeDMD(line)
			lat1, err1 := ParseLatitudeDMS(line)

			switch {
			case err0 == nil:
				loc.LatitudeDMD = lat0
				loc.LatitudeRaw = line
			case err1 == nil:
				loc.LatitudeDMS = lat1
				loc.LatitudeRaw = line
			case strings.Contains(line, emptyLatitude):
				loc.LatitudeRaw = line
				continue
			default:
				return LocationP{}, fmt.Errorf("failed on latitude <<<%s>>>", line)
			}
		}
		if strings.Contains(line, "Long") {
			lon0, err0 := ParseLongitudeDMD(line)
			lon1, err1 := ParseLongitudeDMS(line)

			switch {
			case err0 == nil:
				loc.LongitudeDMD = lon0
				loc.LongitudeRaw = line
			case err1 == nil:
				loc.LongitudeDMS = lon1
				loc.LongitudeRaw = line
			case strings.Contains(line, emptyLongitude):
				loc.LongitudeRaw = line
				continue
			default:
				return LocationP{}, fmt.Errorf("failed on longitude %s", line)
			}
		}
	}
	return loc, nil
}

var (
	latPatternDMS = regexp.MustCompile(`(\d+)°(\d+)´(\d+)´´([NS])?`)
	latPatternDM  = regexp.MustCompile(`(\d+)°(\d+)´([NS])?`)
	latPatternD   = regexp.MustCompile(`(\d+)°([NS])?`)

	lonPatternDMS = regexp.MustCompile(`(\d+)°(\d+)´(\d+)´´([EW])?`)
	lonPatternDM  = regexp.MustCompile(`(\d+)°(\d+)´([EW])?`)
	lonPatternD   = regexp.MustCompile(`(\d+)°([EW])?`)
	lonPatternM   = regexp.MustCompile(`(\d+)´([EW])?`) // Pattern for minutes only
)

func convertLatitudeToDecimal(latitude string) float64 {
	// Convert different degree symbols to a standard one
	latitude = strings.ReplaceAll(latitude, "º", "°")
	latitude = strings.ReplaceAll(latitude, " ", "")

	var degrees, minutes, seconds float64
	var hemisphere string

	// Try to match the DMS pattern (degrees, minutes, seconds)
	if matches := latPatternDMS.FindStringSubmatch(latitude); matches != nil {
		degrees, _ = strconv.ParseFloat(matches[1], 64)
		minutes, _ = strconv.ParseFloat(matches[2], 64)
		seconds, _ = strconv.ParseFloat(matches[3], 64)
		hemisphere = matches[4]
	} else if matches := latPatternDM.FindStringSubmatch(latitude); matches != nil {
		// Try to match the DM pattern (degrees, minutes)
		degrees, _ = strconv.ParseFloat(matches[1], 64)
		minutes, _ = strconv.ParseFloat(matches[2], 64)
		hemisphere = matches[3]
	} else if matches := latPatternD.FindStringSubmatch(latitude); matches != nil {
		// Try to match the D pattern (degrees only)
		degrees, _ = strconv.ParseFloat(matches[1], 64)
		hemisphere = matches[2]
	} else {
		// Return 0 if no pattern matches
		return 0
	}

	// Calculate the decimal degrees
	decimal := degrees + (minutes / 60) + (seconds / 3600)

	// Apply the hemisphere sign
	if hemisphere == "S" {
		decimal = -decimal
	}

	return decimal
}

func convertLongitudeToDecimal(longitude string) float64 {
	// Convert different degree symbols to a standard one
	longitude = strings.ReplaceAll(longitude, "º", "°")
	longitude = strings.ReplaceAll(longitude, " ", "")

	var degrees, minutes, seconds float64
	var hemisphere string

	// Try to match the DMS pattern (degrees, minutes, seconds)
	if matches := lonPatternDMS.FindStringSubmatch(longitude); matches != nil {
		degrees, _ = strconv.ParseFloat(matches[1], 64)
		minutes, _ = strconv.ParseFloat(matches[2], 64)
		seconds, _ = strconv.ParseFloat(matches[3], 64)
		hemisphere = matches[4]
	} else if matches := lonPatternDM.FindStringSubmatch(longitude); matches != nil {
		// Try to match the DM pattern (degrees, minutes)
		degrees, _ = strconv.ParseFloat(matches[1], 64)
		minutes, _ = strconv.ParseFloat(matches[2], 64)
		hemisphere = matches[3]
	} else if matches := lonPatternD.FindStringSubmatch(longitude); matches != nil {
		// Try to match the D pattern (degrees only)
		degrees, _ = strconv.ParseFloat(matches[1], 64)
		hemisphere = matches[2]
	} else if matches := lonPatternM.FindStringSubmatch(longitude); matches != nil {
		// Try to match the minutes-only pattern
		minutes, _ = strconv.ParseFloat(matches[1], 64)
		hemisphere = matches[2]
	} else {
		// Return 0 if no pattern matches
		return 0
	}

	// Calculate the decimal degrees
	decimal := degrees + (minutes / 60) + (seconds / 3600)

	// Apply the hemisphere sign
	if hemisphere == "W" {
		decimal = -decimal
	}

	return decimal
}
