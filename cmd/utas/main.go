package main

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/utils"

	"github.com/carlohamalainen/antarctic-database-go"
)

func main() {
	xs, err := ats.ReadMissingWPsCsv("data/raw/wps_missing.csv")
	if err != nil {
		panic(err)
	}

	urls := []string{}

	for _, d := range xs {
		if d.URL != "" {
			urls = append(urls, d.URL)
		}
	}

	slices.Sort(urls)
	urls = slices.Compact(urls)

	launcherURL := launcher.New().
		Headless(false).
		Set("disable-web-security").
		Set("ignore-certificate-errors").
		Set("plugins.always_open_pdf_externally", "1").
		MustLaunch()

	browser := rod.New().ControlURL(launcherURL).MustConnect()
	defer browser.MustClose()

	for i, url := range urls {
		filename := path.Base(url)
		fmt.Println(i)
		fmt.Println(url)
		fmt.Println(filename)

		page := browser.MustPage(url)
		wait := browser.MustWaitDownload()

		page.MustWaitStable()
		page.MustWaitLoad()
		page.MustWaitStable()

		var agreeButton *rod.Element
		err = rod.Try(func() {
			agreeButton = page.MustElement("button.c-btn-submit")
		})
		if err != nil {
			panic(err)
		}
		if agreeButton == nil {
			panic("no agree button")
		}

		agreeButton.MustClick()

		page.MustWaitNavigation()
		page.MustWaitLoad()
		page.MustWaitStable()

		currentURL := page.MustInfo().URL
		fmt.Printf("Current URL after clicking Agree: %s\n", currentURL)

		if strings.Contains(strings.ToLower(page.MustInfo().URL), ".pdf?token=") {
			fmt.Println("PDF loaded in browser, attempting to download...")

			page.Eval(`
					(() => {
							// Create a temporary anchor element
							const a = document.createElement('a');
							a.href = window.location.href;
							a.download = 'document.pdf';
							a.style.display = 'none';
							document.body.appendChild(a);
							a.click();
							document.body.removeChild(a);
					})()
			`)

			_ = utils.OutputFile(filename, wait())

		} else {
			panic("hmm")
		}
	}
}
