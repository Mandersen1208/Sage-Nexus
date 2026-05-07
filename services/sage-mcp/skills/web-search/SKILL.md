---
name: web-search
description: Guidelines for using the web_search tool to look up current documentation, error messages, library versions, and real-world examples via SearXNG. Use when knowledge cutoff or loaded context is insufficient. Do NOT use for questions already answerable from loaded skills or active conversation context.
metadata:
  author: matt
  version: "1.0.0"
---

# Web Search Skill

Practical rules for deciding when to search, how to query effectively, and how to use results. Search is a limited resource — use it precisely and purposefully.

## When to Use

Search when the answer depends on information that may have changed or isn't in loaded context:

- **Stale docs risk** — library APIs, framework behavior, config file formats that change between versions
- **Version-specific info** — "does X work in Spring Boot 3.4?" or "what's the Hibernate 6 equivalent of Y?"
- **Error messages** — specific stack traces or exception messages not resolvable from context
- **Current events / releases** — latest stable version, recently announced deprecations, recent CVEs
- **Real-world examples** — when you need to verify that a pattern actually works in production, not just in theory
- **Third-party integrations** — OAuth flows, webhook specs, API rate limits that differ by provider

## When NOT to Use

Do not search for things you can already answer:

- Questions answered by a loaded skill (Clean Code, React Best Practices, Postgres, etc.)
- General coding patterns and language fundamentals (Java streams, Python comprehensions, etc.)
- Anything already established in the active conversation
- Questions about Sage's own capabilities or configuration
- Hypothetical or design questions where no authoritative external source exists
- Math, logic, or reasoning tasks

**Default to your training.** Search is for filling gaps, not for validation.

## Tool Reference

### `web_search`

Search the web via SearXNG. Returns text snippets — no JavaScript rendering, no page interaction.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | yes | Search query, 1–200 chars |
| `max_results` | integer | no | Max results to return (default: 5, max: 10) |

**Response format:**

```json
{
  "query": "string",
  "results": [
    {
      "title": "string",
      "url": "string",
      "snippet": "string",
      "engine": "string"
    }
  ],
  "count": 0
}
```

- `snippet` is plain text extracted from the page — may be truncated
- `engine` indicates which underlying search engine returned the result (google, bing, duckduckgo, etc.)
- Empty `results` array means no results found, not an error
- Tool errors (SearXNG unreachable, timeout) surface as MCP error responses

## Query Formulation

### Rules

- **1–6 words.** Shorter queries return more focused results. Long queries match poorly.
- **Specific over generic.** Include the library name, version, language, or framework.
- **Error-first for exceptions.** Lead with the exception class or error code, not a description of it.
- **No natural language.** Drop filler: "how to", "what is", "can I", "I want to"
- **No context from the conversation.** Queries are sent to external search engines — never include user names, project names, internal URLs, credentials, or any detail the user hasn't made public.

### Examples

| Instead of... | Use... |
|---------------|--------|
| `how to configure cors in spring boot` | `spring boot cors configuration` |
| `what is the latest version of react` | `react latest stable version` |
| `java NullPointerException when calling stream().filter()` | `java stream filter NullPointerException` |
| `hibernate 6 deprecation of criteria api` | `hibernate 6 criteria api changes` |
| `supabase rls policy for authenticated users select` | `supabase rls authenticated select policy` |

### Version Pinning

When version matters, include it in the query:

```
jackson 2.17 serialize LocalDate
spring security 6 oauth2 resource server
next.js 15 app router fetch cache
```

## Result Handling

### Reading Results

- Lead with the snippet. If the snippet answers the question, use it — don't hallucinate that a full page read is needed.
- Prefer results from: official docs, GitHub issues/PRs, Stack Overflow accepted answers, well-known engineering blogs.
- Deprioritize: SEO content farms, tutorial aggregators with no original content, anything with "10 best X" in the title.
- If multiple results conflict, prefer the most recent one and note the discrepancy.

### Citing Results

Always cite the source when using search results:

- For factual claims: include the URL inline or as a footnote.
- For code examples pulled from results: attribute the source.
- Don't present search-derived content as your own prior knowledge.

### When Results Are Insufficient

If results don't answer the question:

1. Try a narrower query (remove terms, add specifics)
2. Try a different angle (e.g., search for the error code instead of the behavior)
3. Tell the user what you found and what's still unclear — don't fabricate

**Two searches max per question** before telling the user you couldn't find a reliable answer.

## Rate Limiting and Courtesy

- **One search per question** as the default. Don't scatter-search with five variations hoping one hits.
- Don't search the same query twice in a session.
- Don't search to confirm things you already know — that's search tourism.
- SearXNG is self-hosted and shared. Treat it like a shared resource, not a free API.

## Security Rules

Never include in any search query:

- User names, email addresses, usernames, or account identifiers
- Project names, internal service names, or hostnames (unless they're public open-source repos)
- API keys, tokens, secrets, or any credential fragment
- Internal URLs, IP addresses, or Tailscale hostnames
- Business logic details, schema names, or proprietary terminology
- Anything the user said in private context that they haven't explicitly made public

If a useful query would require including sensitive context, **describe what you're looking for in generic terms** or ask the user to provide a sanitized version.

## Error Handling

| Condition | Response |
|-----------|----------|
| SearXNG unreachable / timeout | Tell the user search is unavailable. Answer from training if possible, otherwise say so. |
| Empty results | Try one reformulated query. If still empty, say so and answer from training. |
| Results are low quality / off-topic | Note it, answer from training, flag uncertainty. |
| Query would leak sensitive info | Do not search. Answer from training or ask for a sanitized query. |

Don't retry indefinitely. One reformulation attempt, then move on.
