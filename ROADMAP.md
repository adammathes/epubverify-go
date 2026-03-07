# Roadmap

## DATE UPDATED

February 28, 2026

---

## STATUS

### Test Results

| Suite | Result | Notes |
|-------|--------|-------|
| **Godog BDD scenarios** | 923/924 passing (1 pending) | 100% pass rate on non-pending scenarios |
| **Unit tests** | All passing | 35 doctor tests, 39 content model tests, epub/validate tests |
| **Stress tests** | 200+ EPUBs configured (prev 77/77 match) | 5 sources: Gutenberg, IDPF, Standard Ebooks, Feedbooks, EPUB2 |
| **Diverse stress test** | 84 EPUBs from 10+ sources, 73/84 match (86.9%) | KBNL, Thorium, DAISY, Readium, bmaupin, pmstss, Pressbooks, manga, filesamples, hand-crafted edge cases |
| **Crawl stress test** | 19 EPUBs crawled, 18/19 match (94.7%) | 4 sources live-tested: Gutenberg, Standard Ebooks, Feedbooks, OAPEN |
| **Synthetic EPUBs** | 29/29 match epubcheck | Purpose-built edge cases |
| **Toolchain EPUBs** | 20/20 match epubcheck (100%) | Pandoc (9) + Calibre (10) + EPUB2 (1) |

### Where We Have Confidence

**High confidence — EPUB 3.3 validation core.** The 923 passing BDD scenarios are ported directly from epubcheck's own test suite and cover OCF container checks, OPF package document validation, XHTML/SVG/SMIL content document checks, navigation document validation, CSS checks, media overlay validation, fixed-layout checks, accessibility checks, cross-reference resolution, encoding detection, EPUB Dictionary/Index/Preview collections, EDUPUB profile checks, and Search Key Map validation. The stress test corpus of 200+ real-world EPUBs (from Project Gutenberg, IDPF samples, Standard Ebooks, Feedbooks, and EPUB2 variants) is configured for comparison against epubcheck 5.1.0. Previous round of 77 EPUBs matched 100%.

**High confidence — Continuous crawl stress testing.** Automated EPUB discovery and validation pipeline with five source crawlers: Gutenberg, Standard Ebooks, Feedbooks, OAPEN, and Internet Archive. Live-tested against real endpoints: 19 EPUBs crawled and validated, 18/19 (94.7%) agreement rate with epubcheck. One false positive (RSC-012/HTM-017 over-reporting on an OAPEN scholarly EPUB). Pipeline includes SHA-256 dedup, rate limiting, validator comparison, and summary reporting. Weekly GitHub Actions workflow for steady-state coverage, manual runs for deep dives.

**High confidence — EPUB 2.0.1 validation.** 7 feature files covering NCX, OCF, OPF, and OPS checks for EPUB 2. The stress test corpus includes EPUB 2 books.

**High confidence — Doctor mode.** 24 fix types across 4 tiers, all with unit tests and integration tests that take broken EPUBs to 0 errors.

**High confidence — HTML5 content model (RSC-005).** The Tier 1 RelaxNG gap analysis closed 62 of 63 identified gaps. We now enforce: block-in-phrasing (div inside p/h1-h6/span/etc.), restricted children (ul/ol/table/select/dl/hgroup), void element children, table content model, interactive nesting, transparent content model inheritance, figcaption position, and picture structure. Only remaining gap: `<input>` type-specific attribute validation (low priority).

**High confidence — Schematron rule coverage (Tier 2).** The Tier 2 Schematron audit covers all 118 patterns from epubcheck's 10 core .sch files: 167 checks implemented, 13 partial, 0 missing. New checks added: disallowed descendant nesting (address/form/progress/meter/caption/header/footer/label), required ancestors (area→map, img[ismap]→a[href]), bdo dir attribute, SSML ph nesting, duplicate map names, select multiple validation, meta charset uniqueness, link sizes validation, and IDREF attribute checking.

**High confidence — Java code check coverage (Tier 3).** The Tier 3 Java audit covers all 315 MessageId codes defined in epubcheck: 223 implemented (including EPUB Dictionaries, EDUPUB, collections), 9 suppressed (disabled in epubcheck), 83 wontfix (niche/defunct features like DTBook, scripting checks, dead code). See `scripts/java-audit.py` (run with `--json` for machine-readable output).

### Where We Have Less Confidence

**Medium confidence — Edge-case error codes.** A handful of epubcheck error codes rely on schema validation or features we haven't implemented: RSC-007 (mailto links), RSC-020 (CFI URLs), OPF-007c (prefix redeclaration), PKG-026 (font obfuscation), OPF-043 (complex fallback chains).

**Medium confidence — Non-English and exotic EPUBs.** The test corpus now includes 20+ non-English EPUBs: French, German, Italian, Spanish, Russian, Chinese, Japanese, Korean, Persian, Portuguese, Greek, Esperanto, and Hindi/Sanskrit. CJK vertical text and FXL are covered by IDPF samples.

**Lower confidence — Very large EPUBs.** Testing has been on typical-sized books. The corpus includes Bible, Complete Shakespeare, Encyclopaedia Britannica, and multi-volume works, but memory usage on 50MB+ EPUBs is untested.

### Progress History

Started at 605/903 (67%) → 826/903 (91.6%) → 867/902 → 901/902 → **923/924 (100% non-pending)**

---

## APPROVED

(No items currently approved. See PROPOSED for candidates.)

---

## PROPOSED



### Update Doctor Mode

Given the more comprehensive tests, offer more fixes for checks that have a clear easy to implement adjustment to resolve in an epub.

### Doctor Mode BDD Tests

Add Gherkin scenarios for doctor mode. Currently only tested via Go unit tests. BDD scenarios would make the expected behavior more visible and could serve as documentation.

### Performance Benchmarking at Scale

Extend `make bench` to run against the full stress test corpus. Track per-book validation time, memory usage, startup overhead (JVM vs native Go), and batch throughput.

### Expand Crawl Sources

The crawler currently covers Gutenberg, Standard Ebooks, Feedbooks, OAPEN, and Internet Archive. Add more diverse sources to increase coverage of edge cases:

- **Open Library** — lending library EPUBs via Open Library API. Modern publisher output.
- **ManyBooks.net** — community-contributed EPUBs, varied quality.
- **Smashwords/Draft2Digital** — indie-published EPUBs with diverse toolchain output.
- **Non-English OPDS catalogs** — e.g. Gallica (French), Wikisource exports, Asian digital libraries.

Goal: catch edge cases that well-curated sources don't exercise (broken metadata, unusual encodings, exotic CSS, deeply nested TOCs, very large files).

### Enable Scheduled EPUB Crawl Workflow

The `.github/workflows/epub-crawl.yml` workflow is currently manual-only (`workflow_dispatch`). After manual testing confirms the crawl pipeline works end-to-end, uncomment the `schedule` block to enable weekly automated runs (Sundays 06:00 UTC).

### Additional Toolchain EPUB Generation Testing

Extend the toolchain test suite (pandoc + calibre) with more EPUB generators to increase structural diversity. Each tool produces different OPF layouts, metadata patterns, and content document structures.

**High value (easy to install, widely used):**

- **ebooklib** (Python, `pip install ebooklib`) — Popular programmatic EPUB library used by many server-side apps. Writes its own OPF, NCX, and content documents from scratch with a distinct internal structure.
- **asciidoctor-epub3** (Ruby, `gem install asciidoctor-epub3`) — Generates EPUB 3 from AsciiDoc markup. Entirely different toolchain (Ruby) with its own nav/OPF generation path.
- **Sphinx** (Python, `pip install sphinx`) — Python documentation tool with EPUB builder. Common in technical documentation; exercises RST-to-EPUB conversion.

**Medium value (heavier install, more niche):**

- **tex4ebook** — LaTeX to EPUB via TeX4ht. Requires full TeX installation (~2GB+). Produces very different output: math-heavy content, complex CSS, unusual element structures.
- **WeasyPrint** / other HTML-to-EPUB converters — Various smaller tools with their own quirks.

**Difficult to automate (GUI-based):**

- **Sigil** — Most popular open-source EPUB editor. Qt GUI app, hard to automate headlessly. Could manually export a few representative test EPUBs.

Goal: each new tool exercises different code paths and structural patterns, increasing the chance of catching false positives/negatives in epubverify.

### CI Integration for Stress Tests

Add a CI job that downloads a cached set of test EPUBs, runs epubverify, compares against cached epubcheck results, and fails if any new disagreements appear.

---

## COMPLETED

### Install Pandoc and Calibre for EPUB Generation Testing — Complete

Installed pandoc and calibre (ebook-convert) to generate EPUBs from scratch using different toolchains. Generated 20 EPUBs (9 pandoc, 10 calibre, 1 EPUB 2) from 6 diverse source content files, validated all against both epubverify and epubcheck 5.3.0.

**Results: 20/20 verdict match (100% agreement)**

**Source content (6 files):**

| File | What it tests |
|------|---------------|
| `basic-prose.md` | Standard Markdown formatting, footnotes, tables, code blocks |
| `multilingual.md` | 10 languages including CJK, Arabic RTL, Cyrillic, Greek |
| `math-content.md` | LaTeX math → MathML via pandoc |
| `complex-structure.md` | Deep nesting, definition lists, multiple authors, edge cases |
| `minimal.md` | Minimal document — bare minimum content |
| `rich-html.html` | HTML5 semantic elements, tables with tfoot, inline styling |

**Generated EPUBs (20 total):**

| Tool | Count | Configurations |
|------|-------|----------------|
| Pandoc | 9 | Default EPUB3, EPUB2, MathML, TOC, chapter split, custom CSS, from HTML |
| Calibre | 10 | From HTML, from Markdown, styled, multilingual, with TOC, with cover, full metadata, no default styling |
| EPUB 2 | 1 | Pandoc EPUB 2 output |

**Bugs found and fixed (2 false negatives):**

| Bug | Check | Fix |
|-----|-------|-----|
| HTML5 elements (`<article>`, `<footer>`, `<mark>`, `<time>`, `<aside>`, `<header>`) in EPUB 2 content not caught by full-EPUB validation | RSC-005 | Added `checkHTML5ElementsEPUB2()` call in `checkContentWithSkips()` for `Version < "3.0"` |
| Colon-containing `id` attributes (`fn:1`, `fnref:2`) generated by calibre not caught | RSC-005 | Added `checkInvalidIDValues()` function to detect non-NCName id values |

**Deliverables:**
- Generation script: `stress-test/toolchain-epubs/generate-epubs.sh` (supports `--pandoc-only`, `--calibre-only`)
- Validation script: `stress-test/toolchain-epubs/validate-epubs.sh` (verdict comparison + error code diff)
- 6 source content files in `stress-test/toolchain-epubs/source-content/`
- 10 new unit tests in `pkg/validate/content_test.go` (3 for invalid IDs, 7 for HTML5 elements in EPUB 2)
- 6 stress tests in `test/stress/toolchain_test.go` (script existence, source diversity, feature checks)
- Makefile targets: `toolchain-generate`, `toolchain-validate`, `toolchain-all`
- Generated EPUBs and results are gitignored (regenerable)

### Verify and Fix Crawl Sources End-to-End — Complete

All five crawler sources tested against live endpoints. Bugs found and fixed:

**Bugs fixed:**

| Bug | Script | Fix |
|-----|--------|-----|
| Standard Ebooks OPDS catalog requires Patrons Circle authentication | `epub-crawler.sh` | Replaced OPDS catalog with paginated ebooks listing page scraping |
| Standard Ebooks direct downloads return HTML without `?source=feed` | `epub-crawler.sh` | Added `?source=feed` query parameter to download URLs |
| Internet Archive search API returns non-JSON when blocked/unavailable | `epub-crawler.sh` | Added JSON validation and curated fallback identifier list |
| Python heredoc can't read bash variables (not exported) | `crawl-validate.sh` | Added `export` for `EPUB_DIR`, `MANIFEST`, `RESULTS_DIR`, `EPUBVERIFY`, `EPUBCHECK_JAR`, `LIMIT` |
| `--output FILE` flag prints message but doesn't write report to file | `crawl-report.sh` | Added file-writing logic in Python and exported `OUTPUT` variable |
| OAPEN TODO comments left from development | `epub-crawler.sh` | Removed stale TODO (source works correctly) |

**Live test results (19 EPUBs):**

| Source | Downloaded | Validated | Agreement |
|--------|-----------|-----------|-----------|
| Gutenberg | 5 | 5/5 match | 100% |
| Standard Ebooks | 5 | 5/5 match | 100% |
| Feedbooks | 5 | 5/5 match | 100% |
| OAPEN | 4 | 3/4 match, 1 FP | 75% |
| Internet Archive | 0 (archive.org blocked in sandbox) | N/A | N/A |
| **Total** | **19** | **18/19 match (94.7%)** | |

**False positive detail:** OAPEN scholarly book (`9783966650663.epub`) — epubverify over-reports RSC-012 (fragment identifier) and HTM-017 (undeclared entity) on content that epubcheck accepts. Root cause: different handling of entity references in XHTML content documents with non-standard entities.

**Internet Archive note:** The `archive.org` domain was blocked by the sandbox egress policy (HTTP 403, `x-block-reason: hostname_blocked`). The crawler code includes proper API parsing and a curated fallback list of popular IA identifiers, but needs testing in an unrestricted environment.

### Long-Running EPUB Scouring Stress Test — Complete

Automated EPUB discovery, validation, and discrepancy reporting pipeline with three components and a scheduled GitHub Actions workflow.

**Components:**

| Component | File | Purpose |
|-----------|------|---------|
| **Crawler** | `scripts/epub-crawler.sh` | Discovers and downloads EPUBs from Gutenberg, Standard Ebooks, Feedbooks |
| **Validator** | `scripts/crawl-validate.sh` | Runs both epubverify and epubcheck, compares verdicts |
| **Reporter** | `scripts/crawl-report.sh` | Generates summary reports, files GitHub issues for discrepancies |
| **CI Workflow** | `.github/workflows/epub-crawl.yml` | Weekly scheduled crawl + validate + report pipeline |

**Crawler features:**
- SHA-256 deduplication — never re-downloads or re-tests the same EPUB
- Rate limiting (configurable, default 2s between requests)
- Respectful User-Agent header
- JSON manifest tracking (`stress-test/crawl-manifest.json`, gitignored)
- Day-seed rotation — each day crawls a different range of source IDs
- EPUBs never committed to repo (stored in gitignored `stress-test/crawl-epubs/`)
- Dry-run mode for testing

**Validator features:**
- Runs both epubverify and epubcheck on each EPUB
- Classifies discrepancies: false positive (epubverify over-reports) vs false negative (epubverify misses errors)
- Updates manifest with verdicts and match status
- Handles crashes gracefully

**Reporter features:**
- Human-readable summary with agreement rates
- Per-source breakdown
- Error code frequency analysis
- False positive/negative detail with specific check IDs
- GitHub issue filing via `gh` CLI (`--file-issues` flag)

**GitHub Actions workflow:**
- Weekly schedule (Sundays 06:00 UTC) + manual dispatch
- Configurable source and limit via workflow inputs
- Caches manifest across runs
- Uploads crawl results as artifacts (30-day retention)
- Automatic issue filing on scheduled runs

**Makefile targets:**
- `make crawl` — run the crawler
- `make crawl-validate` — validate crawled EPUBs
- `make crawl-report` — generate discrepancy report
- `make crawl-all` — full pipeline

**Testing:**
- 16 new Go tests in `test/stress/crawl_test.go` validating: script existence, executability, required features (sources, rate limiting, SHA-256, User-Agent), manifest schema roundtrip, workflow schedule, Makefile targets
- All tests passing alongside existing 7 stress test source tests (23 total)

### Systematic Gap Extraction from Epubcheck's Three Validation Tiers — Complete

All three tiers analyzed and closed. See detailed notes in the APPROVED section history (moved here for reference).

**Summary:**
- **Tier 1 (RelaxNG):** 62/63 content model gaps closed. 8 new check functions, 29 unit tests.
- **Tier 2 (Schematron):** 118/118 patterns accounted for (167 implemented, 13 partial). 8 new check functions, 28 unit tests.
- **Tier 3 (Java):** 315/315 message IDs accounted for (223 implemented, 9 suppressed, 83 wontfix). 19 new check functions, 22 BDD scenarios.

**Audit scripts:** `scripts/relaxng-audit.py`, `scripts/schematron-audit.py`, `scripts/java-audit.py` (all support `--json` for machine-readable output).

**Consolidated known gaps (intentionally skipped):**
- `<input>` type-specific attributes (13+ variants, low real-world impact)
- SVG/MathML full content models (complex, rarely triggers)
- Multi-rendition container patterns (experimental spec extension)
- 51 codes SUPPRESSED in epubcheck itself
- 8 dead code / already-covered codes
- CHK-001 through CHK-007 (epubcheck internal)

### Increase Real-World EPUB Test Coverage — Complete

Expanded the stress test corpus from 77 to 200+ EPUBs across 5 diverse source categories:

| Source | Count | What it tests |
|--------|-------|---------------|
| **Project Gutenberg** | ~105 | Ebookmaker EPUB3 output, diverse content |
| **IDPF/W3C Samples** | 18 | FXL, MathML, SVG, media overlays, CJK, RTL |
| **Standard Ebooks** | 30 | High-quality EPUB3, rich accessibility metadata, se:* vocabulary |
| **Feedbooks** | 20 | Calibre-generated output patterns |
| **EPUB2 Variants** | 17 | Legacy EPUB 2.0.1 validation paths (NCX, OPF 2.0, OPS) |

**Diversity dimensions covered:**
- **Non-English**: 20+ books in French, German, Italian, Spanish, Russian, Chinese, Japanese, Korean, Persian, Portuguese, Greek, Esperanto, Hindi/Sanskrit
- **Large EPUBs**: Bible, Complete Shakespeare, Encyclopaedia Britannica, multi-volume histories
- **Tool output patterns**: Gutenberg Ebookmaker, Calibre/Feedbooks, Standard Ebooks toolchain

**Deliverables:**
- Expanded `stress-test/download-epubs.sh` with 5 source functions (`--gutenberg`, `--idpf`, `--standardebooks`, `--feedbooks`, `--epub2`)
- Updated `stress-test/epub-sources.txt` catalog (200 entries, 0 duplicates)
- New Go test suite `test/stress/sources_test.go` (7 tests): minimum count, diversity, non-English, EPUB2, large EPUBs, unique URLs, download script consistency
- Updated `stress-test/README.md` with corpus documentation

### Tier 3 Java Code Analysis — Complete (Proposal 2)

Java code audit script (`scripts/java-audit.py`) — parses epubcheck's `MessageId.java` (315 message IDs), `DefaultSeverities.java` (severity mappings), and `MessageBundle.properties` (message texts). Greps all Java source files for `MessageId.XXX` references to map every error code to its emitting Java class. Cross-references against epubverify Go source and BDD features. **All 315 message IDs accounted for: 223 implemented, 9 suppressed, 83 wontfix, 0 missing.**

**Phase 1 — 4 new check functions:**

| Check | What it catches |
|-------|----------------|
| `checkLinkNotInManifest` | OPF-067: resource listed as both metadata link and manifest item |
| `checkEmptyMetadataElements` | OPF-072: empty dc:source metadata elements |
| `checkCSSFontFaceUsage` | CSS-028: use of @font-face declaration (USAGE report) |
| `checkSpinePageMap` (extended) | OPF-062: Adobe page-map attribute on spine element |

**Phase 2 — Known Gaps Closure (22 codes implemented):**

Systematically closed all "Could implement later", "EPUB Dictionaries/Index", and "EDUPUB/defunct profiles" gaps:

- **CSS/Content checks:** CSS-006 (position:fixed), HTM-045 (empty href), HTM-051 (microdata/RDFa), HTM-052 (region-based), NCX-004 (uid whitespace)
- **Package document:** OPF-066 (pagination source), OPF-077 (data-nav in spine), OPF-097 (unreferenced items), RSC-019 (multi-rendition metadata)
- **Collections:** OPF-071 (index XHTML), OPF-075 (preview content), OPF-076 (preview CFI)
- **EPUB Dictionaries:** OPF-078 (dict content), OPF-079 (dict dc:type), OPF-080 (SKM extension), OPF-081 (dict resource), OPF-082 (multiple SKM), OPF-083 (no SKM), OPF-084 (invalid resource), RSC-021 (SKM spine)
- Enhanced Collection type with Links field; added collection link parsing in OPF reader

- Machine-readable gap analysis: run `python3 scripts/java-audit.py --json`
- 22 new BDD scenarios, 0 regressions: 923/924 BDD scenarios passing

### Tier 2 Schematron Rule Analysis — Complete (Proposal 2)

Schematron audit script (`scripts/schematron-audit.py`) — parses epubcheck's 10 core Schematron .sch files covering 118 patterns and 180 individual checks. **All patterns accounted for: 167 implemented, 13 partial, 0 missing.**

**8 new check functions added:**

| Check | What it catches |
|-------|----------------|
| `checkDisallowedDescendants` | Forbidden nesting: address/form/progress/meter/caption/header/footer/label |
| `checkRequiredAncestor` | area requires map; img[ismap] requires a[href] |
| `checkBdoDir` | bdo missing required dir attribute |
| `checkSSMLPhNesting` | Nested ssml:ph attributes |
| `checkDuplicateMapName` | Duplicate map name values |
| `checkSelectMultiple` | Multiple selected without @multiple |
| `checkMetaCharset` | Duplicate meta charset elements |
| `checkLinkSizes` | sizes attribute on non-icon link |

- 28 new unit tests for Schematron checks, all passing
- 43 XHTML patterns implemented, 13 IDREF patterns mapped to existing checkIDReferences
- 15 multi-rendition/collection patterns marked wontfix (very niche features, OPF equivalents exist)
- 0 regressions on initial implementation

### Tier 1 RelaxNG Gap Analysis — Complete (Proposal 2)

RelaxNG schema audit script (`scripts/relaxng-audit.py`) — parses epubcheck's 34 RelaxNG .rnc schemas, extracts 115 element definitions and content model rules, compares against epubverify implementation. Initial audit identified **63 content model gaps → reduced to 1** (input type-specific attribute validation, low priority).

**8 new content model check functions:**

| Check | What it catches |
|-------|----------------|
| `checkBlockInPhrasing` | Block elements in phrasing-only parents (p, h1-h6, span, em, etc.) |
| `checkRestrictedChildren` | Invalid children of ul/ol, dl, hgroup, tr, thead/tbody/tfoot, select, etc. |
| `checkVoidElementChildren` | Child elements inside void elements (br, hr, img, input, etc.) |
| `checkTableContentModel` | Non-table children directly inside table |
| `checkInteractiveNesting` | Interactive elements nested in other interactive elements |
| `checkTransparentContentModel` | Transparent content model inheritance (a, ins, del, object, etc.) |
| `checkFigcaptionPosition` | Figcaption not first or last child of figure |
| `checkPictureContentModel` | Picture element structure (source* then img) |

- 29 unit tests for content model checks, all passing
- 16 test fixtures in `testdata/fixtures/relaxng-gaps/xhtml/`
- 0 regressions on initial implementation
- Only remaining gap: `<input>` type-specific attribute validation (low priority, 13+ type variants)

### Validation Engine (PRs #17–#22)

- Full EPUB 3.3 and 2.0.1 validation: OCF, OPF, XHTML, SVG, CSS, SMIL, navigation, accessibility, encoding, fixed-layout, cross-references
- 901/902 godog BDD scenarios passing (ported from epubcheck 5.3.0 test suite)
- Single-file validation support (`.opf`, `.xhtml`, `.svg`, `.smil`)
- Viewport meta tag parsing per EPUB 3.3 spec
- All 41 previously-failing complex scenarios fixed (DOCTYPE, entity refs, SVG, MathML, SSML, epub:switch, microdata, custom namespaces, URL conformance, prefix declarations)

### Doctor Mode (PR #21)

- 24 automatic fix types across 4 tiers (OCF/OPF/XHTML/CSS)
- Non-destructive: always writes to new file, re-validates output
- 35 unit tests + 5 integration tests
- Supports encoding transcoding (ISO-8859-1, Windows-1252, UTF-16 → UTF-8)

### Testing Infrastructure (PRs #23–#25)

- Self-contained godog/Gherkin test suite (no external repos needed)
- Stress test infrastructure: download scripts, comparison runner, analysis tools
- 77/77 real-world EPUBs match epubcheck verdict across 11 testing rounds
- 29 synthetic edge-case EPUBs
- Schematron audit script for coverage gap analysis
- Fuzzing tools (`cmd/epubfuzz/`)
- CI: GitHub Actions with godog BDD tests, Go version matrix

### Bug Fixes from Real-World Testing (11 Rounds)

Over 11 rounds of real-world testing, 30+ false-positive bugs were found and fixed:

| Round | EPUBs | Sources | Bugs Fixed |
|-------|-------|---------|------------|
| 1 | 5 | Gutenberg | 4 (OPF-037, CSS-002, HTM-015, NAV-010) |
| 2 | 25 | +Feedbooks | 4 (OPF-037, E2-007, OPF-036, RSC-002) |
| 3 | 30 | +Gutenberg EPUB2 | 0 |
| 4 | 42 | +IDPF epub3-samples | 7 (CSS-001, OPF-024, HTM-013, HTM-020, HTM-031, MED-004) |
| 5 | 49 | +DAISY, bmaupin | 0 |
| 6 | 86 | +28 IDPF, +11 Gutenberg | 7 (RSC-003, HTM-013, HTM-008, OPF-037, CSS-007, OPF-024, OPF-029) |
| 7 | 96 | +Gutenberg, +IDPF old | 0 |
| 8 | 122 | +Standard Ebooks | 3 (OPF-037, CSS-002, HTM-015) |
| 9 | 133 | +wareid, +readium | 0 |
| 10 | 162 | +29 synthetic | 5 (RSC-001, RSC-002, MED-001, HTM-005, RSC-004) |
| 11 | 77 new | Independent stress test | 3 (OPF-025b, RSC-005, RSC-032) |

### Documentation & Developer Experience

- AGENTS.md with TDD workflow guidelines
- Testing strategy doc with full bug history
- Spec update runbook for adding/debugging tests
- Doctor mode design docs
- Stress test README
