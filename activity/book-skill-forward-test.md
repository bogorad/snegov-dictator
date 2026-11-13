# Book-summary forward test — final acceptance

2026-11-13. Owner: Astra forward-test reader. Both requested fixture outputs are complete. The optional website instruction delta has also passed a bounded readback. No substantive core skill defect was demonstrated. Two test-output/check omissions were corrected before final release.

## Delivered work and exact scope

| Request | Canonical result | Coverage |
| --- | --- | --- |
| A: Russian fiction EPUB tree | [English summary](../tmp/book-skill-forward/fiction/output/summary.md) and [catalog](../tmp/book-skill-forward/fiction/output/catalog.md) | Six unchanged source files. Four ordered chapters across two parts. All 22 body blocks, including 12 narrative paragraphs and one empty paragraph. Front matter and part/chapter headings have explicit dispositions. |
| B: English nonfiction Markdown | [English summary](../tmp/book-skill-forward/nonfiction/output/summary.md) and [catalog](../tmp/book-skill-forward/nonfiction/output/catalog.md) | One unchanged source file. All 24 lines and 1,566 bytes. Source introduction, both chapters, and definitions appendix are accounted for. |

The [detailed report](../tmp/book-skill-forward/fiction/output/skill-review.md) records source interpretation, instruction evidence, actual findings, and limits. Source maps, coverage ledgers, chapter reviews, and local checkpoints are beside each summary. The missing EPUB container file is disclosed; the supplied OPF, NCX, spine, and content establish the available narrative structure.

## Findings retained

- Fiction: the cashier records 28 silver circles for seven crates at three each. The computed total is 21. The source leaves the cause unexplained. Niko's observed survival disproves the death notice; his rescue account and authority remain uncertain. Jan's witnessed killing remains distinct. The wedding chronology agrees. The future Council decision remains unknown.
- Nonfiction: the survey's 60 and 148 respondents sum to 208; its 18 percent comparison remains noncausal and self-reported. The pilot's 40-to-30 reduction is 25%, not one third. Both periods have 0.5 errors per session. The colleague's universal prediction remains separate from the author's cautious recommendation.

## Corrections and final checks

The first fixture checker merged an empty paragraph with the next one. Raw-source comparison exposed that defect. The corrected parser and separate empty-element count now account for all 22 blocks. The storm paragraph was present in the summary throughout. The playbook already warns about this failure.

Root then found an untranslated source title in the fiction introduction. The initial acceptance had missed this session's English-only gate. A scan also found source-script names and labels in the supporting fixture Markdown. These were removed or expressed in English; precise links to unchanged source metadata and passages remain. No story fact, chapter identity, finding, or uncertainty classification changed. Nonfiction needed no content correction.

This language miss is an output/check omission, not a substantive skill gap. [PLAYBOOK.md](/home/chuck/.dotfiles/agents/skills/book-summary/PLAYBOOK.md) lines 142–145 require the requested language and preserve original text through locators/source views. Its Verification matrix at line 1599 requires script scanning plus semantic language review. The corrected fixture verifier enforces this task's English-only contract. It does not impose a universal ban on quoted originals in future tasks.

Final checks pass:

- Exact original source-file sets and SHA-256 hashes; no source changes.
- Source-derived chapter identities/order; complete body or line ownership; negative tests for missing, duplicate, and reversed chapters.
- Catalog metadata; all 95 local links and supplied fragments: 61 fiction, 34 nonfiction.
- UTF-8 and Cyrillic scan of all 11 authored fixture Markdown files: six fiction, five nonfiction. No Cyrillic remains. Manual readback checked English wording and meaning; a script scan alone cannot prove language correctness.
- Complete initial source-to-summary review, followed by readback of every changed language passage. Both final local checkpoints were read back as complete.

Current artifact identities and replayable proof: [fiction verification](../tmp/book-skill-forward/fiction/output/verification.txt) and [nonfiction verification](../tmp/book-skill-forward/nonfiction/output/verification.txt). Corrected fiction summary SHA-256: `0b0b06fe175376ea1409937e152e029281158bbdc5d8a6997084c7e30205c28e`. Nonfiction summary remains `62e81637332513657051f1d01834a030339caa2bef260cc0f52c6e6b0cd2a67c`.

## Optional website delta

Read exactly PLAYBOOK.md lines 1753–1762 and 1809–1812. The changes cover relative favicon URLs after history navigation, optional HTMX event fields, real browser history testing, and the distinction between file-copy/hash proof and live public routing. Upload authority does not grant server-configuration authority. These additions stay within the optional website workflow and do not alter the tested core. Accepted for release; no unrelated behavioral rerun is claimed for this delta.

## Boundaries and limits

No source edits, production Beads writes, Hindsight, Jev, other-model calls, network operations, external messages, Git operations, or skill-file edits were made. Real tracker closure and external reports remain root-owned. Files are local only. No child agents, processes, or terminal sessions remain running for this test.

The fixture verifier is source-specific. This run does not prove arbitrary EPUB parsing, inline chapter anchors, OCR/PDF, table extraction, cache-corruption rejection, long-book scale, parallel merging, fresh-context recovery, live tracker behavior, rendered-site behavior, or publication. Semantic review is same-reader source review plus root's bounded output observation; no full independent second review of the fixture summaries is claimed. The website change received instruction readback, not browser or remote testing in this lane.
