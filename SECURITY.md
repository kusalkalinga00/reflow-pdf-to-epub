# Security policy

## Supported versions

The project has no long-term branches. Fixes go to `main`; a tagged release gets
one if the issue is serious. If you are running an old image, rebuild from
`main` before reporting — the answer may already be there.

## Reporting a vulnerability

Please **do not open a public issue** for a vulnerability.

Use GitHub's private reporting: **Security → Report a vulnerability** on
[this repository](https://github.com/kusalkalinga00/reflow-pdf-to-epub/security/advisories/new).
That opens a draft advisory only you and the maintainers can see. If you cannot
use it, email `kusalkalingainfo@gmail.com` with "Reflow security" in the subject.

Include what you can:

- what you did, with a minimal PDF or request if possible;
- what happened, and what you expected;
- the version (`reflow version`, or the image digest);
- whether it needs a malformed PDF, a crafted upload, or only network access.

What to expect: an acknowledgement within a few days, a decision on severity,
and credit in the advisory unless you prefer otherwise. This is a small project
maintained by one person — there is no bounty.

## Threat model

Reflow is a **self-hosted tool with no accounts and no authentication**. That is
a design decision, not an oversight, and it defines what counts as a
vulnerability.

**In scope**

- A malformed or hostile **PDF** that escapes the parser: reads local files,
  executes code, or consumes unbounded memory or CPU beyond the documented
  limits.
- A **crafted upload** that escapes the request limits, writes outside the
  temporary directory, or retrieves another user's conversion result.
- A **generated EPUB** that carries active content into a reader — script,
  remote resource, `javascript:` URL, or an external entity.
- The **container image** shipping something it should not, or running as root.
- A **dependency** with a reachable vulnerability (`govulncheck` runs in CI).

**Out of scope**

- Anyone who can reach the port can convert PDFs and download the results. Put
  it behind an authenticating proxy, or keep it on loopback; that is the
  operator's job, and the README says so.
- Denial of service by uploading the largest allowed file repeatedly. Limits
  exist (`-max-mb`, `-jobs`), and the answer is the proxy's rate limit.
- Bugs in PDFium, Tesseract, EPUBCheck or the JRE itself — report those
  upstream. We will bump the pinned versions when a fix lands.
- Unsupported PDFs producing an honest low score. The quality report exists to
  name what was lost; a bad T3 is not a vulnerability.

## How the project protects itself

- Every conversion is validated with EPUBCheck before it is written; a book that
  fails is refused, not handed over.
- Uploads are bounded by size, page count and concurrency, and are converted in
  memory.
- The image runs as a non-root user, and the service binds to loopback by
  default, warning when it does not.
- Workflows run with `contents: read` by default, write permissions are scoped
  per job, and third-party actions are pinned to commit SHAs.
- Dependabot watches Go modules, workflows and the Dockerfile; `govulncheck`
  runs on every pull request.
