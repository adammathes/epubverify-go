#!/bin/bash
# Generate EPUBs using pandoc and calibre (ebook-convert) from source content.
#
# Creates EPUBs with different toolchains and configurations to test
# epubverify against diverse real-world output.
#
# Usage: bash stress-test/toolchain-epubs/generate-epubs.sh [OPTIONS]
#
# Options:
#   --pandoc-only     Only generate pandoc EPUBs
#   --calibre-only    Only generate calibre EPUBs
#   --help            Show this help
#
# Prerequisites:
#   - pandoc (apt install pandoc)
#   - calibre (apt install calibre) for ebook-convert

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SOURCE_DIR="${SCRIPT_DIR}/source-content"
OUTPUT_DIR="${SCRIPT_DIR}/epubs"

DO_PANDOC=true
DO_CALIBRE=true

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pandoc-only)  DO_CALIBRE=false; shift ;;
    --calibre-only) DO_PANDOC=false; shift ;;
    --help)
      sed -n '2,/^$/{ s/^# //; s/^#$//; p }' "$0"
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

# Verify prerequisites
if ${DO_PANDOC} && ! command -v pandoc &>/dev/null; then
  echo "ERROR: pandoc not found. Install with: apt install pandoc" >&2
  exit 1
fi
if ${DO_CALIBRE} && ! command -v ebook-convert &>/dev/null; then
  echo "ERROR: ebook-convert (calibre) not found. Install with: apt install calibre" >&2
  exit 1
fi

mkdir -p "${OUTPUT_DIR}"

total=0
failed=0

generate() {
  local tool="$1"
  local name="$2"
  local output="${OUTPUT_DIR}/${name}.epub"
  shift 2
  local cmd=("$@")

  total=$((total + 1))
  echo -n "  [${total}] ${tool}: ${name}... "

  if "${cmd[@]}" 2>/dev/null; then
    if [ -f "${output}" ]; then
      local size
      size=$(stat -c%s "${output}" 2>/dev/null || stat -f%z "${output}" 2>/dev/null)
      echo "OK ($(( size / 1024 ))KB)"
    else
      echo "FAIL (no output file)"
      failed=$((failed + 1))
    fi
  else
    echo "FAIL (exit code $?)"
    failed=$((failed + 1))
  fi
}

echo "=== Generating toolchain EPUBs ==="
echo ""

# ---------------------------------------------------------------------------
# Pandoc EPUBs
# ---------------------------------------------------------------------------
if ${DO_PANDOC}; then
  echo "--- Pandoc EPUBs ---"

  # 1. Basic prose (EPUB 3, default settings)
  generate "pandoc" "pandoc-basic-prose" \
    pandoc "${SOURCE_DIR}/basic-prose.md" \
      -o "${OUTPUT_DIR}/pandoc-basic-prose.epub" \
      --metadata title="Basic Prose Document"

  # 2. Basic prose as EPUB 2
  generate "pandoc" "pandoc-basic-prose-epub2" \
    pandoc "${SOURCE_DIR}/basic-prose.md" \
      -o "${OUTPUT_DIR}/pandoc-basic-prose-epub2.epub" \
      -t epub2

  # 3. Multilingual content
  generate "pandoc" "pandoc-multilingual" \
    pandoc "${SOURCE_DIR}/multilingual.md" \
      -o "${OUTPUT_DIR}/pandoc-multilingual.epub"

  # 4. Math content with MathML
  generate "pandoc" "pandoc-math-mathml" \
    pandoc "${SOURCE_DIR}/math-content.md" \
      -o "${OUTPUT_DIR}/pandoc-math-mathml.epub" \
      --mathml

  # 5. Complex structure
  generate "pandoc" "pandoc-complex-structure" \
    pandoc "${SOURCE_DIR}/complex-structure.md" \
      -o "${OUTPUT_DIR}/pandoc-complex-structure.epub"

  # 6. Minimal document
  generate "pandoc" "pandoc-minimal" \
    pandoc "${SOURCE_DIR}/minimal.md" \
      -o "${OUTPUT_DIR}/pandoc-minimal.epub"

  # 7. HTML input
  generate "pandoc" "pandoc-from-html" \
    pandoc "${SOURCE_DIR}/rich-html.html" \
      -o "${OUTPUT_DIR}/pandoc-from-html.epub" \
      --metadata title="Rich HTML Content"

  # 8. With table of contents
  generate "pandoc" "pandoc-with-toc" \
    pandoc "${SOURCE_DIR}/basic-prose.md" \
      -o "${OUTPUT_DIR}/pandoc-with-toc.epub" \
      --toc --toc-depth=3

  # 9. Chapter splitting by top-level headers
  generate "pandoc" "pandoc-chapter-split" \
    pandoc "${SOURCE_DIR}/basic-prose.md" \
      -o "${OUTPUT_DIR}/pandoc-chapter-split.epub" \
      --split-level=1

  # 10. With custom CSS embedded
  CSS_TMP=$(mktemp /tmp/epub-css-XXXXXX.css)
  cat > "${CSS_TMP}" <<'CSSEOF'
body { font-family: "Palatino Linotype", Palatino, Georgia, serif; }
h1 { text-align: center; margin-top: 3em; }
h2 { border-bottom: 1px solid #ccc; }
p { text-indent: 1.5em; margin: 0; }
blockquote { font-style: italic; border-left: 3px solid #999; }
code { font-family: "Courier New", monospace; background: #f4f4f4; }
CSSEOF
  generate "pandoc" "pandoc-custom-css" \
    pandoc "${SOURCE_DIR}/basic-prose.md" \
      -o "${OUTPUT_DIR}/pandoc-custom-css.epub" \
      --css="${CSS_TMP}"
  rm -f "${CSS_TMP}"

  echo ""
fi

# ---------------------------------------------------------------------------
# Calibre EPUBs
# ---------------------------------------------------------------------------
if ${DO_CALIBRE}; then
  echo "--- Calibre EPUBs ---"

  # 1. HTML to EPUB 3 (default)
  generate "calibre" "calibre-from-html" \
    ebook-convert "${SOURCE_DIR}/rich-html.html" \
      "${OUTPUT_DIR}/calibre-from-html.epub" \
      --title "Rich HTML Content" \
      --authors "Test Author" \
      --language en

  # 2. Markdown to EPUB via calibre
  generate "calibre" "calibre-basic-prose" \
    ebook-convert "${SOURCE_DIR}/basic-prose.md" \
      "${OUTPUT_DIR}/calibre-basic-prose.epub" \
      --title "Basic Prose Document" \
      --authors "Test Author" \
      --language en \
      --input-encoding utf-8

  # 3. With custom styling
  generate "calibre" "calibre-styled" \
    ebook-convert "${SOURCE_DIR}/rich-html.html" \
      "${OUTPUT_DIR}/calibre-styled.epub" \
      --title "Styled Content" \
      --authors "Test Author" \
      --language en \
      --extra-css "body { font-family: serif; } h1 { text-align: center; }"

  # 4. Multilingual
  generate "calibre" "calibre-multilingual" \
    ebook-convert "${SOURCE_DIR}/multilingual.md" \
      "${OUTPUT_DIR}/calibre-multilingual.epub" \
      --title "Multilingual Content Test" \
      --authors "Test Author" \
      --language en \
      --input-encoding utf-8

  # 5. Complex structure
  generate "calibre" "calibre-complex-structure" \
    ebook-convert "${SOURCE_DIR}/complex-structure.md" \
      "${OUTPUT_DIR}/calibre-complex-structure.epub" \
      --title "Complex Structure" \
      --authors "First Author" \
      --language en \
      --input-encoding utf-8

  # 6. Minimal
  generate "calibre" "calibre-minimal" \
    ebook-convert "${SOURCE_DIR}/minimal.md" \
      "${OUTPUT_DIR}/calibre-minimal.epub" \
      --title "Minimal Document" \
      --authors "Test" \
      --language en

  # 7. With TOC
  generate "calibre" "calibre-with-toc" \
    ebook-convert "${SOURCE_DIR}/basic-prose.md" \
      "${OUTPUT_DIR}/calibre-with-toc.epub" \
      --title "With Table of Contents" \
      --authors "Test Author" \
      --language en \
      --level1-toc "//h:h1" \
      --level2-toc "//h:h2" \
      --input-encoding utf-8

  # 8. With cover (auto-generated)
  generate "calibre" "calibre-with-cover" \
    ebook-convert "${SOURCE_DIR}/basic-prose.md" \
      "${OUTPUT_DIR}/calibre-with-cover.epub" \
      --title "Book With Cover" \
      --authors "Test Author" \
      --language en \
      --input-encoding utf-8

  # 9. No default styling
  generate "calibre" "calibre-no-styling" \
    ebook-convert "${SOURCE_DIR}/rich-html.html" \
      "${OUTPUT_DIR}/calibre-no-styling.epub" \
      --title "No Default Styling" \
      --authors "Test Author" \
      --language en \
      --no-default-epub-cover

  # 10. With publisher and other metadata
  generate "calibre" "calibre-full-metadata" \
    ebook-convert "${SOURCE_DIR}/basic-prose.md" \
      "${OUTPUT_DIR}/calibre-full-metadata.epub" \
      --title "Full Metadata Test" \
      --authors "Test Author" \
      --language en \
      --publisher "Test Publisher" \
      --comments "A test book with full metadata." \
      --tags "test,epub,validation" \
      --input-encoding utf-8

  echo ""
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo "=== Generation Summary ==="
echo "  Total attempted: ${total}"
echo "  Succeeded: $((total - failed))"
echo "  Failed: ${failed}"
echo "  Output directory: ${OUTPUT_DIR}"
echo ""

if [ ${failed} -gt 0 ]; then
  echo "WARNING: ${failed} EPUB(s) failed to generate." >&2
fi

ls -la "${OUTPUT_DIR}"/*.epub 2>/dev/null | awk '{printf "  %-40s %s\n", $NF, $5}'
