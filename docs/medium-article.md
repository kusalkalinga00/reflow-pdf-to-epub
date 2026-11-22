# I built a free PDF-to-EPUB converter because reading PDFs on my phone was miserable

*Reflow turns PDF books into real ebooks — and tells you honestly what it could not keep.*

---

I read a lot of books on my phone. Apple Books is where I do it: I can change the
font size, switch to dark mode at night, highlight a sentence, look up a word,
and pick up exactly where I stopped.

PDFs break all of that.

A PDF is not really a book. It is a picture of a page, with every letter placed
at an exact position. On a phone that means constant pinching and zooming,
sideways scrolling, and text that never quite fits the screen. The words are
there, but the reading experience is gone.

I had a folder of PDF books I actually wanted to read. What I needed was simple:
turn them into EPUB files, the format Apple Books reads properly.

So I tried the online converters. Some wanted a subscription for anything more
than a few pages. Some uploaded the file, thought for a while, and gave me
something worse than the PDF — broken sentences, chapters in the wrong order,
or a file Apple Books refused to open. And none of them told me whether the
result was good. I only found out after I started reading.

That is why I built Reflow.

## What Reflow does

Reflow is a small program that converts a PDF into an EPUB. You give it a book,
it gives you a book — with real, selectable text, chapters in the right order,
and a table of contents you can navigate.

It was built for the ordinary case: a PDF book that has a text layer. Novels,
technical books, reports, manuals. The kind of file you download and then never
read because reading it on a phone is unpleasant.

What you get in Apple Books:

- text that reflows to your font size, instead of a page you have to zoom;
- dark mode and night reading that actually work;
- chapters and a proper table of contents, taken from the PDF's own outline;
- search, highlights, bookmarks and notes;
- clickable links, and footnotes kept as footnotes;
- tables kept as tables, where the PDF drew them as tables;
- the original page numbers kept as a map, so a reference to page 42 has
  somewhere to go.

![The original PDF: fixed pages and tiny text](images/pdf-source.png)

*The PDF I wanted to read. Perfectly fine on a large screen, painful on a phone.*

![The same book in Apple Books after Reflow](images/apple-books.png)

*The same book after conversion: my font size, dark mode, real chapters.*

## The part most converters hide

Here is the uncomfortable truth about PDF to EPUB: it is not a perfectly
solvable problem.

A PDF does not say "this is a heading" or "this is a footnote" or "these two
lines are one paragraph". It only says where each letter sits on the page.
Rebuilding a book from that is guesswork — educated guesswork, but guesswork.

Most converters either pretend it went perfectly, or quietly drop things.

Reflow's rule is the opposite: **it measures itself, and it tells you what it
lost.**

Every conversion comes with a short report:

- a score out of 100 and a plain-language verdict;
- how many chapters and headings it found;
- whether it had to read any pages with OCR (recognised from an image);
- what it could not keep — missing figures, flattened tables, unreadable pages.

If the result is poor, it says so, by name. You are never left wondering why a
chapter looks odd. I would rather tell you "this book came out badly" than hand
you a broken book and let you discover it at page 200.

![The quality report after converting a book](images/quality-report.png)

*The report that comes with every book: a score, a verdict, and the losses by name.*

## What it is not

I want to be clear about the limits, because overselling this would waste your
time:

- **Scanned PDFs are a different problem.** If a book is just photographs of
  pages, Reflow tries OCR to read the text. If it cannot, it gives you a
  page-image book: faithful to look at, but not reflowable. The report says
  which one you got.
- **Math and very complex layouts are not recovered.** Equations, magazine
  layouts and overlapping sidebars are beyond it for now.
- **Not every book becomes perfect.** A clean novel usually comes out very
  well. A dense textbook with five kinds of footnotes may come out merely
  readable. The report is how you know.

That honesty is the feature. A converter that claims 100% is lying to you.

## How to use it

Reflow is open source and free, and it runs on your own computer. Your books are
never uploaded to anyone else's server, there is no account, and there is no
per-book price. A book can be up to 1,000 pages / 100 MB.

If you are comfortable with a terminal, one command starts it:

```sh
docker run --rm -p 127.0.0.1:8080:8080 \
  ghcr.io/kusalkalinga00/reflow-pdf-to-epub:latest
```

Then open <http://localhost:8080>, drop in a PDF, and press Convert. The EPUB
downloads to your computer, and you can send it to Apple Books.

If that paragraph looked like a foreign language: this is the part to hand to a
technical friend, or to come back to when you have ten quiet minutes. The README
walks through it step by step. There is no subscription and nothing to buy.

## The sample

I converted one of my own books as a sample so you can see the result before
installing anything.

[**Download the sample EPUB →**](https://github.com/kusalkalinga00/reflow-pdf-to-epub)

Open it in Apple Books (or any reader) and compare it with the PDF it came
from. Same words, different reading experience.

## Why I am sharing it

I built Reflow for myself, because the alternatives were paid, broken, or
dishonest about their output. It has since been tested against a small shelf of
real books — a 300-page technical book, a 250-page novel, a slide deck, a CV and
an invoice — and it is now good enough that I use it for everything I read.

If you have a PDF shelf you have been avoiding, try one book. If the report says
the result is good, it usually is. If it says otherwise, at least you know.

The code is on GitHub, under the Apache 2.0 licence. Issues, ideas and sample
PDFs are welcome — especially the ones that come out badly, because those are
the ones that make it better.

[**github.com/kusalkalinga00/reflow-pdf-to-epub →**](https://github.com/kusalkalinga00/reflow-pdf-to-epub)

---

*If this saved you a book, the best thing you can do is convert another one and
tell me what broke.*
