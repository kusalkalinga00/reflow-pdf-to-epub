# Contributing

Thanks for looking. Reflow is a small project with one rule that matters more
than style: **never claim what you did not measure.**

## Getting set up

```sh
make deps     # pinned PDFium SDK and EPUBCheck into .pdfium/ and .epubcheck/
make check    # gofmt, tidy, vet, tests with -race
make run -- serve
```

`make deps` needs a C toolchain and `pkg-config`. Tesseract is optional; without
it, scans stay empty and the report says so.

## What a change needs

- **A test that fails before it and passes after.** Any change in behaviour
  needs one; the corpus and unit tests are how a claim is checked.
- **The quality report updated** when recovery changes. If a new feature
  recovers something, say so in the report; if it cannot, say that instead.
- **No silent loss.** A thing that cannot be carried into the EPUB is named in
  the report, not dropped.
- **Corpus books stay private.** `corpus/private/` is gitignored and must stay
  that way. Never commit a real book, its ground truth, or a screenshot of one.

## Commits and pull requests

- Small, focused commits, each one complete on its own.
- Commit messages: `feat(scope): ...`, `fix(scope): ...`, `docs: ...`,
  `chore: ...`, then a body explaining why, not what.
- One change per pull request. A pull request body says what was wrong, what
  changed, and how it was verified — with the actual output.
- Keep the tree green: `make check` before pushing.

## Code

- Go 1.27, standard library first, dependencies justified.
- Comments explain *why*; the code explains what.
- Packages under `internal/` are stages of the pipeline. A stage takes the
  document model, does one thing, and returns it; it does not reach around
  another stage.
- Deterministic output: no timestamps, map iteration order, or randomness in
  anything that reaches a book.

## Reporting security problems

Do not open an issue. See [SECURITY.md](SECURITY.md).

## Licence

By contributing you agree your work is licensed under Apache-2.0, the licence of
this project.
