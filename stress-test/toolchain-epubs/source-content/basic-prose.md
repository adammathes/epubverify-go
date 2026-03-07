---
title: "Basic Prose Document"
author: "Test Author"
lang: en
date: 2024-01-15
description: "A basic prose document for EPUB generation testing."
---

# Chapter One: The Beginning

This is a simple prose document designed to test basic EPUB generation.
It contains multiple paragraphs, headings, and standard formatting.

The quick brown fox jumped over the lazy dog. This sentence contains
every letter of the alphabet, making it useful for font rendering tests.

## Section 1.1: Formatting

Here we test **bold text**, *italic text*, and ***bold italic*** together.
We also test `inline code`, ~~strikethrough~~, and [a link](https://example.com).

> This is a blockquote. It should be rendered as an indented block
> in the EPUB output. Blockquotes are a common formatting element
> in both fiction and non-fiction works.

## Section 1.2: Lists

An ordered list:

1. First item
2. Second item
3. Third item with a longer description that wraps across
   multiple lines to test paragraph handling

An unordered list:

- Apples
- Oranges
- Bananas
  - Yellow bananas
  - Green bananas

## Section 1.3: Code Block

```python
def hello_world():
    """A simple function."""
    print("Hello, World!")
    return True
```

# Chapter Two: More Content

This chapter exists to ensure multi-chapter navigation works correctly.

## Section 2.1: A Table

| Name    | Age | City      |
|---------|-----|-----------|
| Alice   | 30  | New York  |
| Bob     | 25  | London    |
| Charlie | 35  | Tokyo     |

## Section 2.2: Horizontal Rule

Above the rule.

---

Below the rule.

## Section 2.3: Footnotes

This paragraph has a footnote.[^1] And another one here.[^2]

[^1]: This is the first footnote with some explanatory text.
[^2]: This is the second footnote.

# Chapter Three: Final Chapter

The end of our test document. This ensures the EPUB has at least
three chapters for proper navigation testing.
