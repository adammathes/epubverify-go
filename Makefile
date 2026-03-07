EPUBCHECK_JAR ?= $(HOME)/tools/epubcheck-5.3.0/epubcheck.jar

.PHONY: build test godog-test bench stress-test crawl crawl-validate crawl-report toolchain-generate toolchain-validate toolchain-all clean help

build:                       ## Build the binary
	go build -o epubverify .

test:                        ## Run unit tests
	go test ./pkg/...

godog-test:                  ## Run Gherkin/godog spec compliance tests
	go test ./test/godog/ -v -count=1

test-all: test godog-test    ## Run all tests (unit + godog)

stress-test: build           ## Run stress test against epubcheck on real EPUBs
	bash stress-test/download-epubs.sh --all
	bash stress-test/run-comparison.sh
	bash stress-test/analyze-results.sh

crawl: build                 ## Crawl new EPUBs from public sources
	bash scripts/epub-crawler.sh

crawl-validate: build        ## Validate crawled EPUBs with both validators
	bash scripts/crawl-validate.sh

crawl-report:                ## Generate crawl discrepancy report
	bash scripts/crawl-report.sh

crawl-all: crawl crawl-validate crawl-report  ## Full crawl pipeline

toolchain-generate:          ## Generate EPUBs with pandoc and calibre
	bash stress-test/toolchain-epubs/generate-epubs.sh

toolchain-validate: build    ## Validate toolchain EPUBs with both validators
	bash stress-test/toolchain-epubs/validate-epubs.sh

toolchain-all: toolchain-generate toolchain-validate  ## Full toolchain pipeline

bench: build                 ## Benchmark vs reference epubcheck
	@echo "=== epubverify ===" && time ./epubverify testdata/fixtures/epub3/00-minimal/minimal.epub --json /dev/null 2>/dev/null
	@echo "=== reference java ===" && time java -jar $(EPUBCHECK_JAR) testdata/fixtures/epub3/00-minimal/minimal.epub --json /dev/null 2>/dev/null

clean:
	rm -f epubverify

help:                        ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'
