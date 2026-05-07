---
name: clean-code
description: Clean Code principles for writing readable, maintainable, and professional software. Use this skill when writing, reviewing, or refactoring code in any language. Triggers on tasks involving code generation, code review, refactoring, naming conventions, function design, error handling, test writing, or general code quality improvement. Optimized for Java and Python.
license: MIT
metadata:
  author: community
  version: "1.0.0"
  inspired_by: "Clean Code principles (Robert C. Martin)"
---

# Clean Code Skill

Practical guidelines for writing clean, readable, and maintainable code. Contains 52 rules across 8 categories, prioritized by impact on code quality. Language-agnostic principles with Java and Python examples.

## When to Apply

Reference these guidelines when:
- Writing new functions, classes, or modules
- Reviewing code for readability and maintainability
- Refactoring existing code
- Designing error handling strategies
- Writing or improving unit tests
- Naming variables, functions, classes, or packages
- Deciding whether to add or remove comments

## Rule Categories by Priority

| Priority | Category | Impact | Prefix |
|----------|----------|--------|--------|
| 1 | Naming | CRITICAL | `naming-` |
| 2 | Functions | CRITICAL | `fn-` |
| 3 | Error Handling | HIGH | `err-` |
| 4 | Testing | HIGH | `test-` |
| 5 | Classes & Objects | MEDIUM-HIGH | `class-` |
| 6 | Comments | MEDIUM | `comment-` |
| 7 | Formatting | MEDIUM | `format-` |
| 8 | Boundaries | MEDIUM | `boundary-` |

## Quick Reference

### 1. Naming (CRITICAL)

- `naming-intention` - Names should reveal intent without requiring a comment
- `naming-no-disinformation` - Avoid names that lie about what something is or does
- `naming-distinction` - Make meaningful distinctions, never use noise words
- `naming-pronounceable` - Use pronounceable names you can discuss verbally
- `naming-searchable` - Use names that are easy to grep for, avoid single letters
- `naming-no-encoding` - Don't encode types or scope into names
- `naming-class-noun` - Classes are nouns, never verbs
- `naming-method-verb` - Methods are verbs or verb phrases
- `naming-one-word-per-concept` - Use one word per abstract concept consistently
- `naming-domain-terms` - Use solution domain and problem domain terms appropriately

### 2. Functions (CRITICAL)

- `fn-small` - Functions should be small, then smaller than that
- `fn-one-thing` - A function should do one thing and do it well
- `fn-one-level-of-abstraction` - Statements in a function should be at one level of abstraction
- `fn-args-minimal` - Fewer arguments are better, three is the practical max
- `fn-no-flag-args` - Never pass a boolean to select behavior, split into two functions
- `fn-no-side-effects` - A function should not have hidden side effects
- `fn-command-query-separation` - A function should either do something or answer something, not both
- `fn-prefer-exceptions` - Use exceptions over returning error codes
- `fn-extract-try-catch` - Extract try/catch bodies into their own functions
- `fn-dry` - Don't repeat yourself, duplication is the root of all evil in software

### 3. Error Handling (HIGH)

- `err-exceptions-over-codes` - Use exceptions instead of return codes for error signaling
- `err-unchecked-exceptions` - Prefer unchecked exceptions to avoid dependency chains
- `err-context-in-exceptions` - Provide enough context in exception messages to locate the failure
- `err-caller-defined-exceptions` - Define exception classes by how the caller needs to handle them
- `err-no-null-return` - Don't return null, use Optional, empty collections, or Special Case pattern
- `err-no-null-pass` - Don't pass null as an argument unless the API explicitly requires it
- `err-wrap-third-party` - Wrap third-party APIs to normalize their exception behavior

### 4. Testing (HIGH)

- `test-one-assert-per-concept` - Test one concept per test, not necessarily one assert
- `test-fast` - Tests must run fast or developers will stop running them
- `test-independent` - Tests should not depend on each other or on ordering
- `test-repeatable` - Tests must produce the same result in any environment
- `test-self-validating` - Tests should return boolean pass/fail, no manual inspection
- `test-timely` - Write tests just before the production code that makes them pass
- `test-clean-tests` - Test code deserves the same quality as production code
- `test-readable` - Tests should read as clear specifications of behavior
- `test-build-operate-check` - Structure tests as build, operate, check (Arrange, Act, Assert)
- `test-domain-language` - Build a domain-specific testing language for readability

### 5. Classes & Objects (MEDIUM-HIGH)

- `class-small` - Classes should be small, measured by responsibility not line count
- `class-single-responsibility` - A class should have one and only one reason to change
- `class-cohesion` - Methods should manipulate a high proportion of the class variables
- `class-organize-for-change` - Structure classes so changes are isolated and low-risk
- `class-prefer-polymorphism` - Prefer polymorphism over switch/if-else type checking
- `class-law-of-demeter` - A method should only talk to its immediate friends
- `class-data-transfer-objects` - Use DTOs for data transfer, keep them free of business logic

### 6. Comments (MEDIUM)

- `comment-why-not-what` - Comments should explain why, never what the code does
- `comment-no-journal` - Don't keep changelogs in comments, that's what git is for
- `comment-no-noise` - Remove comments that restate the obvious
- `comment-no-closing-brace` - Don't comment closing braces, shorten the function instead
- `comment-no-commented-out-code` - Delete commented-out code, version control remembers
- `comment-legal-headers` - Legal headers are acceptable when required
- `comment-todo` - TODO comments are acceptable but must be tracked and resolved
- `comment-warn-consequences` - Warning comments about non-obvious consequences are valuable

### 7. Formatting (MEDIUM)

- `format-vertical-density` - Related code should appear vertically dense
- `format-vertical-distance` - Variables declared close to their usage
- `format-dependent-functions` - Caller above callee, high-level above low-level
- `format-newspaper-rule` - File reads top-to-bottom like a newspaper article
- `format-team-rules` - The team agrees on one set of rules and everyone follows them
- `format-horizontal-limit` - Lines should be short enough to read without scrolling

### 8. Boundaries (MEDIUM)

- `boundary-wrap-third-party` - Wrap third-party APIs behind your own interfaces
- `boundary-learning-tests` - Write tests against third-party code to learn and detect changes
- `boundary-adapter-pattern` - Use adapters when integrating code that doesn't exist yet



## How to Use

Read individual rule files for detailed explanations and code examples:

```
rules/naming-intention.md
rules/fn-small.md
```

Each rule file contains:
- Brief explanation of why it matters
- Bad code example with explanation
- Clean code example with explanation
- Language-specific notes for Java and Python

## Full Compiled Document

For the complete guide with all rules expanded: `AGENTS.md`
