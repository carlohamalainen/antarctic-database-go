package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

//go:generate go run ./tools/structs
//go:generate go run ./tools/metadata

const TXT = ""   // not supported, TODO
const CURR = "0" // not supported, TODO

type Language string

const (
	English Language = "e"
	Spanish Language = "s"
	French  Language = "f"
	Russian Language = "r"
)

type DocumentLink struct {
	Language Language
	Url      string
}

// Measure has the raw HTML document and our attempt to parse characteristics and approvals.
type Measure struct {
	Raw             *goquery.Document
	Title           string
	Characteristics []Characteristic
	Content         string
	Approvals       []Approval
	ApprovalText    *string // Sometimes the approval is a free text field.
}

// Approval is for approvals that have a country and date.
type Approval struct {
	Date    string
	Country string
}

// Characteristic holds parsed tabular data from a measure (the ATS API responds with a html document).
type Characteristic struct {
	Title string
	Text  string
	Url   *string
}

func ParseMeasure(url string, body io.ReadCloser) Measure {
	m := Measure{}

	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		panic(err)
	}

	m.Raw = doc

	m.Title = strings.TrimSpace(doc.Find("h1.title").Text())

	doc.Find(".characteristics__item").Each(func(_ int, s *goquery.Selection) {
		characteristic := Characteristic{}

		title := s.Find(".characteristics__item__title").Text()
		text := s.Find(".characteristics__item__text__text").Text()
		link, _ := s.Find(".characteristics__item__text__link").Attr("href")

		title = strings.TrimSpace(title)
		text = strings.TrimSpace(text)
		link = strings.TrimSpace(link)
		link = strings.ReplaceAll(link, "\\", "/")

		characteristic.Title = title
		characteristic.Text = text

		if link != "" {
			ok, err := ValidateDocumentLink(link)
			if err != nil {
				panic(err)
			}
			if !ok {
				panic("not valid: " + link)
			}

			characteristic.Url = &link
		}

		m.Characteristics = append(m.Characteristics, characteristic)
	})

	s := doc.Find(".main-cols .main-col .text-container")
	content, _ := s.Html()
	content = strings.ReplaceAll(content, "<br/>", "\n")

	// Remove any remaining HTML tags
	docContent, _ := goquery.NewDocumentFromReader(strings.NewReader(content))
	content = docContent.Text()

	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "\n\n\n", "\n\n")
	m.Content = content

	// Sometimes we find a table in the sidebar with dates and countries.
	doc.Find("tr.sidebar-text__text").Each(func(_ int, s *goquery.Selection) {
		country := s.Find(".approvals-th .fa-p").Text()
		date := s.Find(".approvals-td .fa-p").Text()

		country = strings.TrimSpace(country)
		date = strings.TrimSpace(date)

		m.Approvals = append(m.Approvals, Approval{Date: date, Country: country})
	})

	// Sometimes the sidebar is just text.
	selector := "html body#template-10.❄️ section.section div div.main-cols.main-cols--sidebar.cols.single-measure.line--feed--before.line--feed aside.sidebar-col.line--feed--before.line--feed div.sidebar-text.section.characteristics p.sidebar-text__text"
	doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		m.ApprovalText = &text
	})

	if len(m.Characteristics) == 0 && len(m.Approvals) == 0 && m.ApprovalText == nil {
		panic("weird empty measure: " + url)
	}
	return m
}

func BuildSearchURL(meeting Meeting_Date, cat Cat, topic Topic, docType DocType, status Status, page int) string {
	return fmt.Sprintf("https://www.ats.aq/devAS/ToolsAndResources/SearchDatabase?from=%s&to=%s&cat=%s&top=%s&type=%s&stat=%s&txt=%s&curr=%s&page=%d",
		meeting, meeting, cat, topic, docType, status, TXT, CURR, page)
}

func BuildSecondURL(meeting Meeting_Date, cat Cat, topic Topic, docType DocType, status Status, aRecID int) string {
	return fmt.Sprintf("https://www.ats.aq/devAS/Meetings/Measure/%d?s=1&iframe=1&from=%s&to=%s&cat=%s&top=%s&type=%s&stat=%s&txt=%s&curr=%s",
		aRecID, meeting, meeting, cat, topic, docType, status, TXT, CURR)
}

func BuildSearchMeetingDocuments(meetingType MeetingType, meeting Meeting_Integer, party Party, paperType PaperType, category Category, page int) string {
	return fmt.Sprintf(
		"https://www.ats.aq/devAS/Meetings/SearchDocDatabase?meeting=%s&from=%s&to=%s&party=%s&type=%s&category=%s&title=&page=%d",
		meetingType,
		meeting,
		meeting,
		party,
		paperType,
		category,
		page)
}

func DownloadLinks(paper DocumentPayloadItem) []DocumentLink {
	docs := []DocumentLink{}

	link := "https://documents.ats.aq/"
	link += paper.Meeting_type + paper.Meeting_number
	link += "/"
	link += paper.Abbreviation
	link += "/"
	link += paper.Meeting_type + paper.Meeting_number + "_"
	link += paper.Abbreviation
	link += fmt.Sprintf("%03d_", paper.Number)
	if paper.Revision > 0 {
		link += fmt.Sprintf("rev%d_", paper.Revision)
	}

	link = strings.ReplaceAll(link, "\\", "/") // yes, we see backslashes sometimes

	switch {
	case paper.State_en > 1:
		docs = append(docs, DocumentLink{Language: English, Url: link + "e." + paper.Type})
	case paper.State_sp > 1:
		docs = append(docs, DocumentLink{Language: Spanish, Url: link + string(Spanish) + "." + paper.Type})
	case paper.State_fr > 1:
		docs = append(docs, DocumentLink{Language: French, Url: link + string(French) + "." + paper.Type})
	case paper.State_ru > 1:
		docs = append(docs, DocumentLink{Language: Russian, Url: link + string(Russian) + "." + paper.Type})
	}

	return docs
}

func AttachmentLink(attachment DocumentPayloadItemAttachmentsItem) DocumentLink {
	docPath := "https://documents.ats.aq/"

	att := docPath +
		attachment.Meeting_type + attachment.Meeting_number +
		"/att/" +
		attachment.Meeting_type + attachment.Meeting_number +
		"_att" +
		fmt.Sprintf("%03d", attachment.Number) + "_"

	if attachment.Revision > 0 {
		att += fmt.Sprintf("rev%d_", attachment.Revision)
	}

	att += attachment.Att_lang + "." + attachment.Type

	att = strings.ReplaceAll(att, "\\", "/")

	dl := DocumentLink{
		Url: att,
	}

	switch attachment.Att_lang {
	case "e":
		dl.Language = English
	case "s":
		dl.Language = Spanish
	case "f":
		dl.Language = French
	case "r":
		dl.Language = Russian
	default:
		panic("unknown attachment language: " + attachment.Att_lang)
	}

	return dl
}

func ValidateDocumentLink(url string) (bool, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, nil
	}

	contentType := resp.Header.Get("Content-Type")
	validTypes := []string{
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/pdf",
		"application/zip",
		"application/x-zip-compressed",
		"image/png",
	}

	for _, validType := range validTypes {
		if strings.Contains(contentType, validType) {
			return true, nil
		}
	}

	return false, nil
}