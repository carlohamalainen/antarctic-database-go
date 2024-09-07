# antarctic-database-go - a simple Go API to the Antarctic Treaty Database

[![Build Status](https://github.com/carlohamalainen/antarctic-database-go/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/carlohamalainen/antarctic-database-go/actions)
[![Go Reference](https://pkg.go.dev/badge/github.com/carlohamalainen/antarctic-database-go.svg)](https://pkg.go.dev/github.com/carlohamalainen/antarctic-database-go)
[![Sourcegraph Badge](https://sourcegraph.com/github.com/carlohamalainen/antarctic-database-go/-/badge.svg)](https://sourcegraph.com/github.com/carlohamalainen/antarctic-database-go?badge)

antarctic-database-go provides a simple API for querying documents and measures in the [ATS Datbase][atsdb].
Specifically, we support the _Antarctic Treaty Database_ and _Meeting Documents_ databases.

[Antarctic Treaty Database search page][treatydb] description:

> In this database you can find the text of measures adopted by the ATCM (including all Recommendations, Measures, Decisions and Resolutions) from 1961 to now together with their attachments and information on their legal status.
>
> This database contains the text of all Recommendations, Measures, Decisions and Resolutions and other measures adopted by the ATCM together with their attachments and information on their legal status.

[Meeting Documents Archive search page][meetingdb] description:

> A searchable database of all the working documents submitted by Parties, Observers and Experts to the meetings.

## Table of Contents

* [Installation](#installation)
* [Changelog](#changelog)
* [API](#api)
* [Examples](#examples)
* [Related Projects](#related-projects)
* [Support](#support)
* [License](#license)

## Installation

Starting with `v1.0.0` of antarctic-database-go, Go 1.23+ is required.

    $ go get github.com/carlohamalainen/antarctic-database-go

(optional) To run unit tests:

    $ cd $GOPATH/src/github.com/carlohamalainen/antarctic-database-go
    $ go test

## Changelog

*    **2024-09-07 (v1.0.1)** : Tag version 1.0.1.
*    **2024-09-07** Change package name from `api` to `ats`.
*    **2024-09-07 (v1.0.0)** : Tag version 1.0.0.
*    **2024-09-07** Initial commit.

## API

antarctic-database-go exposes ... TODO

The complete [package reference documentation can be found here][doc].

## Examples

TODO

See the [examples directory](./example/).

## Related Projects

- [ats-papers](https://github.com/gcuth/ats-papers), a Python pipeline for scraping and analysing Antarctic Treaty Papers. As of 2021, archived.

## Support

There are a number of ways you can support the project:

* Use it, star it, build something with it, spread the word!
  - If you do build something open-source or otherwise publicly-visible, let me know so I can add it to the [Related Projects](#related-projects) section!
* Raise issues to improve the project (note: doc typos and clarifications are issues too!)
  - Please search existing issues before opening a new one - it may have already been addressed.
* Pull requests: please discuss new code in an issue first, unless the fix is really trivial.
  - Make sure new code is tested.
  - Be mindful of existing code - PRs that break existing code have a high probability of being declined, unless it fixes a serious issue.

## License

The [BSD 3-Clause license][bsd], the same as the [Go language][golic].

[atsdb]: https://www.ats.aq/e/tools-and-resources.html
[bsd]: https://opensource.org/licenses/BSD-3-Clause
[doc]: https://pkg.go.dev/github.com/carlohamalainen/antarctic-database-go
[golic]: https://go.dev/LICENSE
[treatydb]: https://www.ats.aq/devAS/ToolsAndResources/AntarcticTreatyDatabase?lang=e
[meetingdb]: https://www.ats.aq/devAS/Meetings/DocDatabase?lang=e
