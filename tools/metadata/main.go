package main

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/carlohamalainen/antarctic-database-go/cache"

	"unicode"

	"go/token"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"
)

func generateCases(stuff map[string]string) []dst.Stmt {
	keys := slices.Collect(maps.Keys(stuff))
	slices.Sort(keys)

	cases := make([]dst.Stmt, 0, len(stuff)+1)

	for _, name := range keys {
		cases = append(cases, &dst.CaseClause{
			List: []dst.Expr{dst.NewIdent(name)},
			Body: []dst.Stmt{
				&dst.ReturnStmt{
					Results: []dst.Expr{
						&dst.BasicLit{
							Kind:  token.STRING,
							Value: fmt.Sprintf(`"%s"`, name),
						},
					},
				},
			},
		})
	}

	panicInternalError :=
		&dst.ExprStmt{
			X: &dst.CallExpr{
				Fun: dst.NewIdent("panic"),
				Args: []dst.Expr{
					&dst.BasicLit{
						Kind:  token.STRING,
						Value: `"internal error"`,
					},
				},
			},
		}

	cases = append(cases, &dst.CaseClause{
		List: nil, // default case
		Body: []dst.Stmt{panicInternalError},
	})
	return cases
}

func mkDeclarations(prefix string, stuff map[string]string) []dst.Decl {
	decls := []dst.Decl{}

	decls = append(decls, &dst.GenDecl{
		Tok: token.TYPE,
		Specs: []dst.Spec{
			&dst.TypeSpec{
				Name: dst.NewIdent(prefix),
				Type: dst.NewIdent("string"),
			},
		},
		// Decs: dst.GenDeclDecorations{
		//     NodeDecs: dst.NodeDecs{
		//         Before: dst.NewLine,
		//         Start: []string{"// Blah is a type alias for string"},
		//     },
		// },
	})

	keys := slices.Collect(maps.Keys(stuff))
	slices.Sort(keys)

	constDecl := &dst.GenDecl{
		Tok: token.CONST,
	}

	for _, name := range keys {
		value := stuff[name]

		slog.Info("generating const", "type", prefix, "name", name, "value", value)

		s := &dst.ValueSpec{
			Names: []*dst.Ident{dst.NewIdent(name)},
			Type:  dst.NewIdent(prefix),
			Values: []dst.Expr{
				&dst.BasicLit{
					Kind:  token.STRING,
					Value: fmt.Sprintf(`"%s"`, value),
				},
			},
		}

		constDecl.Specs = append(constDecl.Specs, s)
	}

	decls = append(decls, constDecl)

	createKeysList := func(keys []string) []dst.Expr {
		var keysList []dst.Expr
		for _, key := range keys {
			if strings.HasSuffix(key, "_All") {
				continue
			}
			keysList = append(keysList, dst.NewIdent(key))
		}
		return keysList
	}

	// Generate const list of all keys
	keysListName := prefix + "Keys"
	keysListDecl := &dst.GenDecl{
		Tok: token.VAR,
		Specs: []dst.Spec{
			&dst.ValueSpec{
				Names: []*dst.Ident{dst.NewIdent(keysListName)},
				Type: &dst.ArrayType{
					Elt: dst.NewIdent(prefix),
				},
				Values: []dst.Expr{
					&dst.CompositeLit{
						Type: &dst.ArrayType{
							Elt: dst.NewIdent(prefix),
						},
						Elts: createKeysList(keys),
					},
				},
			},
		},
	}
	decls = append(decls, keysListDecl)

	funcDecl := &dst.FuncDecl{
		Name: dst.NewIdent(fmt.Sprintf("%sToString", prefix)),
		Type: &dst.FuncType{
			Params: &dst.FieldList{
				List: []*dst.Field{
					{
						Names: []*dst.Ident{dst.NewIdent("m")},
						Type:  dst.NewIdent(prefix),
					},
				},
			},
			Results: &dst.FieldList{
				List: []*dst.Field{
					{
						Type: dst.NewIdent("string"),
					},
				},
			},
		},
		Body: &dst.BlockStmt{
			List: []dst.Stmt{
				&dst.SwitchStmt{
					Tag: dst.NewIdent("m"),
					Body: &dst.BlockStmt{
						List: generateCases(stuff),
					},
				},
			},
		},
	}

	decls = append(decls, funcDecl)

	return decls

}

func parseStructures(doc *goquery.Document, prefix, id string) []dst.Decl {
	stuff := map[string]string{}
	reverse := map[string]string{}

	selector := fmt.Sprintf("select#%s.form__input", id)

	doc.Find(selector).Each(func(i int, s *goquery.Selection) {
		fmt.Println("Found select element with id 'input-from' and class 'form__input'")

		s.Find("option").Each(func(j int, option *goquery.Selection) {
			value, _ := option.Attr("value")
			text := option.Text()

			text = prefix + "_" + text
			text = sanitizeIdentifier(text)
			text = regexp.MustCompile(`_{2,}`).ReplaceAllString(text, "_")
			text = text[:min(len(text), 80)]

			if _, ok := stuff[text]; ok {
				panic("repeated identifier, the code will not compile: " + text)
			}

			// Many duplicates in the topics list, for example:
			//
			// <option  value="320">Grave of Nicolai Hanson - Cape Adare (HSM 23)</option>
			// <option  value="320">HSM 23 (Grave of Nicolai Hanson - Cape Adare)</option>
			//
			// So we will take the first that we encounter.

			if _, ok := reverse[value]; !ok {
				reverse[value] = text
				stuff[text] = value
			}

		})
	})

	if len(stuff) == 0 {
		panic("did not find any declarations for " + prefix + " " + id)
	}

	return mkDeclarations(prefix, stuff)
}

func parseRadioBoxes(file *dst.File, url string) error {
	client, err := cache.NewHTTPClient(nil)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("failed to create new http client")
	}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("nil response")
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return err
	}

	// Top level radio boxes:
	//
	// Meeting Type:
	// ATCM (Antarctic Treaty Consultative Meeting)
	// SATCM (Special Antarctic Treaty Consultative Meeting)
	// ATME (Meeting of Experts)
	// ATIP (Antarctic Treaty Intersessional Period)

	selector := "html body#template-2.❄️ section.section article.search.container section.js-tabs.tabs div.tabs__content div#tab-1.js-tab-body.tabs__body.-active form#search__form__basic.search__form.form div.form__cols.cols div.form__input-wrapper div.form__input-helper div.form__input-wrapper.form__input-wrapper--radio"

	radioBoxes := map[string]string{}
	meetingPrefix := "MeetingType"

	doc.Find(selector).Each(func(i int, s *goquery.Selection) {
		input := s.Find("input")
		label := s.Find("label")

		id, _ := input.Attr("id")
		value := label.Text()

		idParts := strings.Split(id, "-")
		number := idParts[len(idParts)-1]

		value = meetingPrefix + "_" + value
		value = regexp.MustCompile(`_{2,}`).ReplaceAllString(value, "_")
		value = sanitizeIdentifier(value)
		value = value[:min(len(value), 80)]

		radioBoxes[value] = number
	})

	file.Decls = append(file.Decls, mkDeclarations(meetingPrefix, radioBoxes)...)

	return nil
}

type ScrapeTarget struct {
	Url     string
	Targets []GenerateTarget
}

type GenerateTarget struct {
	GenerateStructName string
	ScrapeInputTarget  string
}

var (
	SearchDatabaseScrapeTarget = ScrapeTarget{
		Url: "https://www.ats.aq/devAS/ToolsAndResources/SearchAtd",
		Targets: []GenerateTarget{
			{
				GenerateStructName: "Meeting_Date", // these meetings have mm/dd/yyyy option values
				ScrapeInputTarget:  "input-from",
			},
			{
				GenerateStructName: "Cat",
				ScrapeInputTarget:  "input-cat",
			},
			{
				GenerateStructName: "Topic",
				ScrapeInputTarget:  "input-top",
			},
			{
				GenerateStructName: "DocType",
				ScrapeInputTarget:  "input-type",
			},
			{
				GenerateStructName: "Status",
				ScrapeInputTarget:  "input-stat",
			},
		},
	}

	MeetingsDocDatabaseScrapeTarget = ScrapeTarget{
		Url: "https://www.ats.aq/devAS/Meetings/DocDatabase",
		Targets: []GenerateTarget{
			{
				GenerateStructName: "Meeting_Integer", // these meetings have integer option values and only cover ATCM
				ScrapeInputTarget:  "input-from",
			},
			{
				GenerateStructName: "Party",
				ScrapeInputTarget:  "input-party",
			},
			{
				GenerateStructName: "PaperType",
				ScrapeInputTarget:  "input-type",
			},
			{
				GenerateStructName: "Category",
				ScrapeInputTarget:  "input-category",
			},
		},
	}
)

func parseTarget(file *dst.File, target ScrapeTarget) {
	slog.Info("downloading", "url", target.Url)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Get(target.Url)
	if err != nil {
		panic(err)
	}
	if resp == nil {
		panic("nil resp")
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		panic(err)
	}

	for _, gen := range target.Targets {
		decls := parseStructures(doc, gen.GenerateStructName, gen.ScrapeInputTarget)
		file.Decls = append(file.Decls, decls...)
	}
}

func sanitizeIdentifier(s string) string {
	if len(s) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteRune(rune(s[0]))

	isValidIdentifierChar := func(ch rune) bool {
		return ch == '_' || unicode.IsLetter(ch) || unicode.IsDigit(ch)
	}

	for _, ch := range s[1:] {
		switch {
		case ch == ' ':
			builder.WriteRune('_')
		case isValidIdentifierChar(ch):
			builder.WriteRune(ch)
		}
	}

	return builder.String()
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	fileD := &dst.File{
		Name: dst.NewIdent("ats"),
		Decs: dst.FileDecorations{
			NodeDecs: dst.NodeDecs{
				Start: []string{"// AUTOGENERATED FILE! Do not edit!", "\n"},
			},
		},
	}

	parseTarget(fileD, SearchDatabaseScrapeTarget)
	parseTarget(fileD, MeetingsDocDatabaseScrapeTarget)
	parseRadioBoxes(fileD, MeetingsDocDatabaseScrapeTarget.Url)

	dstutil.Apply(fileD, nil, nil)

	fff, err := os.Create("metadata.go")
	if err != nil {
		panic(err)
	}
	defer fff.Close()

	if err := decorator.Fprint(fff, fileD); err != nil {
		panic(err)
	}
}
