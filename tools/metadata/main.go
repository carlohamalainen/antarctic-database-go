package main

import (
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"unicode"

	"go/ast"
	"go/format"
	"go/token"
)

func generateCases(stuff map[string]string) []ast.Stmt {
	keys := slices.Collect(maps.Keys(stuff))
	slices.Sort(keys)

	cases := make([]ast.Stmt, 0, len(stuff)+1)

	for _, name := range keys {
		cases = append(cases, &ast.CaseClause{
			List: []ast.Expr{ast.NewIdent(name)},
			Body: []ast.Stmt{
				&ast.ReturnStmt{
					Results: []ast.Expr{
						&ast.BasicLit{
							Kind:  token.STRING,
							Value: fmt.Sprintf(`"%s"`, name),
						},
					},
				},
			},
		})
	}

	panicInternalError :=
		&ast.ExprStmt{
			X: &ast.CallExpr{
				Fun: ast.NewIdent("panic"),
				Args: []ast.Expr{
					&ast.BasicLit{
						Kind:  token.STRING,
						Value: `"internal error"`,
					},
				},
			},
		}

	cases = append(cases, &ast.CaseClause{
		List: nil, // default case
		Body: []ast.Stmt{panicInternalError},
	})
	return cases
}

func mkDeclarations(prefix string, stuff map[string]string) []ast.Decl {
	decls := []ast.Decl{}

	decls = append(decls, &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent(prefix),
				Type: ast.NewIdent("string"),
			},
		},
	})

	constDecl := &ast.GenDecl{
		Tok: token.CONST,
	}

	keys := slices.Collect(maps.Keys(stuff))
	slices.Sort(keys)

	for _, name := range keys {
		value := stuff[name]

		slog.Info("generating const", "type", prefix, "name", name, "value", value)

		constDecl.Specs = append(constDecl.Specs, &ast.ValueSpec{
			Names: []*ast.Ident{ast.NewIdent(name)},
			Type:  ast.NewIdent(prefix),
			Values: []ast.Expr{
				&ast.BasicLit{
					Kind:  token.STRING,
					Value: fmt.Sprintf(`"%s"`, value),
				},
			},
		})
	}

	decls = append(decls, constDecl)

	createKeysList := func(keys []string) []ast.Expr {
		var keysList []ast.Expr
		for _, key := range keys {
			if strings.HasSuffix(key, "_All") {
				continue
			}
			keysList = append(keysList, ast.NewIdent(key))
		}
		return keysList
	}

	// Generate const list of all keys
	keysListName := prefix + "Keys"
	keysListDecl := &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names: []*ast.Ident{ast.NewIdent(keysListName)},
				Type: &ast.ArrayType{
					Elt: ast.NewIdent(prefix),
				},
				Values: []ast.Expr{
					&ast.CompositeLit{
						Type: &ast.ArrayType{
							Elt: ast.NewIdent(prefix),
						},
						Elts: createKeysList(keys),
					},
				},
			},
		},
	}
	decls = append(decls, keysListDecl)

	funcDecl := &ast.FuncDecl{
		Name: ast.NewIdent(fmt.Sprintf("%sToString", prefix)),
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					{
						Names: []*ast.Ident{ast.NewIdent("m")},
						Type:  ast.NewIdent(prefix),
					},
				},
			},
			Results: &ast.FieldList{
				List: []*ast.Field{
					{
						Type: ast.NewIdent("string"),
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.SwitchStmt{
					Tag: ast.NewIdent("m"),
					Body: &ast.BlockStmt{
						List: generateCases(stuff),
					},
				},
			},
		},
	}

	decls = append(decls, funcDecl)

	return decls

}

func parseStructures(doc *goquery.Document, prefix, id string) []ast.Decl {
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

func parseRadioBoxes(file *ast.File, url string) {
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		panic(err)
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
		// Url: "https://www.ats.aq/devAS/ToolsAndResources/SearchAtd",
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

func parseTarget(file *ast.File, target ScrapeTarget) {
	slog.Info("downloading", "url", target.Url)

	resp, err := http.Get(target.Url)
	if err != nil {
		panic(err)
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

	file := &ast.File{
		Name: ast.NewIdent("api"),
	}

	parseTarget(file, SearchDatabaseScrapeTarget)
	parseTarget(file, MeetingsDocDatabaseScrapeTarget)
	parseRadioBoxes(file, MeetingsDocDatabaseScrapeTarget.Url)

	fset := token.NewFileSet()
	var buf strings.Builder
	if _, err := buf.WriteString("// AUTOGENERATED FILE! Do not edit!\n\n"); err != nil {
		panic(err)
	}
	if err := format.Node(&buf, fset, file); err != nil {
		panic(err)
	}

	if err := os.WriteFile("metadata.go", []byte(buf.String()), 0644); err != nil {
		panic(err)
	}
}
