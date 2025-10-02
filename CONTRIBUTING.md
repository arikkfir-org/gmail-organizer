# Contributing to Gmail Organizer

First off, thank you for considering contributing to Gmail Organizer! It's people like you that make open source
software such a great thing.

We welcome any type of contribution, not just code. You can help with:

* **Reporting a bug**
* **Discussing the current state of the code**
* **Submitting a fix**
* **Proposing new features**
* **Becoming a maintainer**

## We Develop with Github

We use Github to host code, to track issues and feature requests, as well as accept pull requests.

Pull requests are the best way to propose changes to the codebase. We actively welcome your pull requests:

1. Fork the repo and create your branch from `main`.
2. If you've added code that should be tested, add tests.
3. If you've changed APIs, update the documentation.
4. Ensure the test suite passes.
5. Make sure your code lints.
6. Issue that pull request!

## Any contributions you make will be under the Apache 2.0 Software License

In short, when you submit code changes, your submissions are understood to be under the
same [Apache 2.0 License](http://www.apache.org/licenses/LICENSE-2.0) that covers the project. Feel free to contact the
maintainers if that's a concern.

## Report bugs using Github's [issues](https://github.com/arikkfir-org/gmail-organizer/issues)

We use GitHub issues to track public bugs. Report a bug by [opening a new issue](); it's that easy!

## Write bug reports with detail, background, and sample code

**Great Bug Reports** tend to have:

* A quick summary and/or background
* Steps to reproduce
    * Be specific!
    * Give sample code if you can.
* What you expected would happen
* What actually happens
* Notes (possibly including why you think this might be happening, or stuff you tried that didn't work)

People *love* thorough bug reports. I'm not even kidding.

## Use a Consistent Coding Style

We use `gofmt` to format our code. Please run it on your code before submitting a pull request.

## Commit Messages

We follow the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) specification. Please ensure your
commit messages are in this format. This allows us to automatically generate changelogs and determine version bumps.

A commit message consists of a **header**, a **body** and a **footer**. The header has a special format that includes a
**type**, a **scope** and a **subject**:

```
<type>(<scope>): <subject>
<BLANK LINE>
<body>
<BLANK LINE>
<footer>
```

The **header** is mandatory and the **scope** of the header is optional.

Any line of the commit message cannot be longer than 100 characters! This allows the message to be easier to read on
GitHub as well as in various git tools.

### Type

Must be one of the following:

* **feat**: A new feature
* **fix**: A bug fix
* **docs**: Documentation only changes
* **style**: Changes that do not affect the meaning of the code (white-space, formatting, missing semi-colons, etc)
* **refactor**: A code change that neither fixes a bug nor adds a feature
* **perf**: A code change that improves performance
* **test**: Adding missing tests or correcting existing tests
* **chore**: Changes to the build process or auxiliary tools and libraries such as documentation generation

### Scope

The scope could be anything specifying place of the commit change. For example `dispatcher`, `worker`, `infra`, etc.

### Subject

The subject contains succinct description of the change:

* use the imperative, present tense: "change" not "changed" nor "changes"
* don't capitalize first letter
* no dot (.) at the end

### Body

Just as in the **subject**, use the imperative, present tense: "change" not "changed" nor "changes". The body should
include the motivation for the change and contrast this with previous behavior.

### Footer

The footer should contain any information about **Breaking Changes** and is also the place to reference GitHub issues
that this commit **Closes**.

**Breaking Changes** should start with the word `BREAKING CHANGE:` with a space or two newlines. The rest of the commit
message is then the description of the change, justification and migration notes.
