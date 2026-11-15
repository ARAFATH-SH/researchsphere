# Evaluation: collaborator matching (NFR-6, AI-2.2), semantic search (AI-5.2) and speed (NFR-1)

What is measured, how, and the results. Run on 2026-11-15 (slice 17, chunk 17.1) and
2026-11-16 (slice 18, chunk 18.1: the last two sections). Each section says which `make`
command reproduces it. All five commands run `cmd/recseval`, read the development database only
and refuse anything else. They need `APP_ENV=development`, which you
can pass on the command line: `make eval-recs APP_ENV=development`.

## Setup

| Item | Value |
| --- | --- |
| Data | 10,000 synthetic sample profiles from `make sample-data n=10000` (slice 16), data seed 16, all embedded |
| Split | 8,012 `tuning`, 1,988 `test` (20% held out, drawn per profile from its seed) |
| Clusters | 20 main research clusters, 450 to 550 profiles each (about 500) |
| Test split mix | 615 with a second cluster, 314 sparse (headline and 2 interests only), 294 with Bangla text |
| Embedding model | `intfloat/multilingual-e5-base@d128750597153bb5987e10b1c3493a34e5a4502a` |
| Index | `profile_embeddings_hnsw_idx` (HNSW, cosine), pgvector 0.8.7, `hnsw.ef_search = 100`, `hnsw.iterative_scan = relaxed_order` |
| Draws | seed 17 (calibration queries, benchmark users) |
| Machine | 8 CPUs, 14 GB, PostgreSQL 16.15 in Docker, the same machine as the client |

**Relevance (NFR-6):** a candidate is relevant to a query profile when both have the same
`main_cluster` in `sample_profiles` (docs/decisions.md, 2026-11-15). A shared second cluster
does not count.

## Score calibration (AI-2.2), `make calibrate-recs`

**Why there is a mapping.** e5 puts almost every pair of profiles between cosine 0.75 and
0.98. Showing the raw cosine as a percentage would make every recommendation look like
"85%". So each recommendation shows

    score = round(100 / (1 + exp(-(slope × cos + intercept))))

This is a logistic curve (Platt scaling). It is fitted by maximum likelihood to estimate the
chance that two profiles at that similarity share their main research area. The slope must be
above 0, so a more similar profile never gets a lower score. The score is clamped to 0 to 100.

**Data, TUNING split only.** The queries are 2,000 tuning profiles, drawn with seed 17. Each
query contributes 100 pairs:
- its 50 nearest tuning profiles, ranked exactly as the endpoint ranks them; this is the
  region recommendations come from;
- 50 random tuning profiles, which anchor the low end.

That gives 200,000 pairs: 96,231 in the same cluster and 103,769 in different ones. No test
profile is a query or a candidate.

**Result:** `slope = 78.7208`, `intercept = -70.2827`. The score is 50 at cosine 0.893.

| Cosine | Pairs | Same cluster | Score at the middle |
| --- | ---: | ---: | ---: |
| 0.74 to 0.76 | 294 | 0.0% | 0 |
| 0.76 to 0.78 | 2,451 | 0.0% | 0 |
| 0.78 to 0.80 | 10,241 | 0.1% | 0 |
| 0.80 to 0.82 | 22,116 | 0.4% | 0 |
| 0.82 to 0.84 | 27,390 | 1.0% | 1 |
| 0.84 to 0.86 | 21,492 | 2.7% | 3 |
| 0.86 to 0.88 | 11,620 | 13.0% | 14 |
| 0.88 to 0.90 | 9,107 | 50.4% | 44 |
| 0.90 to 0.92 | 18,471 | 79.4% | 79 |
| 0.92 to 0.94 | 32,870 | 93.9% | 95 |
| 0.94 to 0.96 | 37,212 | 99.2% | 99 |
| 0.96 to 0.98 | 6,694 | 100.0% | 100 |

**Where the constants live.** They are the defaults of `RECS_SCORE_MODEL`, `RECS_SCORE_SLOPE` and
`RECS_SCORE_INTERCEPT` in `backend/internal/platform/config/config.go`, and they belong to the
model named there.

**Refit rule.** Embeddings of another model are spread differently, so a model change
(`name@revision`) means a refit:
1. Re-embed the sample data.
2. Run `make calibrate-recs`.
3. Put the printed values into the config defaults and `.env.example`.

While the caller's embedding model differs from `RECS_SCORE_MODEL`, the API logs the warning
"recommendation score map was fitted for another embedding model" once per model.

**Caveat.** The curve depends on how the pairs were drawn (half from the nearest 50, half
random), and on synthetic clusters. Read a score as "how strongly this pair looks like
same-area researchers on the sample data", not as a probability for real users.

## Collaborator matching quality (NFR-6), `make eval-recs`

**Method.**
- Each of the 1,988 TEST profiles is a query.
- The candidates are every other embedded sample profile of both splits: 9,999.
- They are ranked exactly as `GET /recommendations/people` ranks:
  - the same HNSW query on the same index;
  - the same cosine operator, model filter, verification and suspension filters, and order;
  - the same `ef_search` and iterative scan (`recommendationpg.SetupRanking`).
- Only the connection and block exclusions are left out. NFR-6 measures matching, not the
  social graph.
- The top k = 10 are kept, the length of the list the endpoint shows.
- Nothing is fitted or tuned here.

**Definitions.** For a query q:
- R(q) is the set of candidates in q's main cluster (q itself excluded). |R(q)| is its size.
- hits(q) is how many of q's top 10 are in R(q).

Each measure below is the mean over all queries.

| Measure | Per query | Meaning |
| --- | --- | --- |
| **Recall@10** (capped, the headline) | hits(q) / min(10, \|R(q)\|) | Share of the relevant candidates a 10-item list could hold that it does hold |
| **Precision@10** | hits(q) / 10 | Share of the 10 shown that are relevant; a list shorter than 10 still divides by 10 |
| **Standard recall@10** | hits(q) / \|R(q)\| | Share of all relevant candidates found in the top 10 |

**Why standard recall is near 0.02 at most.** Each cluster has about 500 profiles, so
|R(q)| ≈ 500. A 10-item list can find at most 10 of them: 10 / 500 = 0.02, even when every
shown item is relevant. That number measures the list length, not the matching. The capped
recall divides by what a list of 10 could hold, so a perfect list scores 1. Here every query
has at least 10 relevant candidates, so capped recall equals precision@10. They would differ
only for a cluster with fewer than 10 other members.

**Second relevance rule, related field.** The NFR-6 rule above is the headline. Beside it, the
same ranking is also measured with a looser rule: a candidate is relevant when it **shares a
research cluster** with q, meaning the main or second cluster of one is the main or second
cluster of the other. Same main cluster is included. About 30% of sample profiles have a second
cluster, so a query has about 850 related candidates instead of about 500. The definitions of
the three measures are unchanged, with R(q) taken under this rule.

**Results (2026-11-15, test split, 1,988 queries, 9,999 candidates):**

| Metric | Same main cluster (NFR-6, headline) | Shared main or second cluster |
| --- | ---: | ---: |
| Mean \|R(q)\| | 500.2 | 847.0 |
| **Recall@10 (capped)** | **0.9585** | 0.9863 |
| Precision@10 | 0.9585 | 0.9863 |
| Standard recall@10 (ceiling ≈ 10 / \|R(q)\|) | 0.0192 | 0.0129 |

**The main-cluster misses.** Of the 19,880 top-10 items (1,988 queries × 10), 825 (4.1%)
are outside the query's main cluster.
- 552 of the 825 (66.9%) share the main or second cluster with the query. They are in a
  related field.
- The other 273 (1.4% of all top-10 items) share no cluster. They come from overlapping
  vocabulary between clusters and the 10% of profiles with noise tags.

**Score distributions.** The score map is the one fitted on the tuning split; it was not
refitted (see below). "Random" pairs are each test query with 200 random sample profiles from
both splits (seed 17), drawn whatever their rank.

| Score of | Pairs | Mean | p10 | p25 | p50 | p75 | p90 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Random pairs, same main cluster | 19,841 | 52.8 | 2 | 13 | 58 | 90 | 97 |
| Random pairs, different main cluster | 377,759 | 3.4 | 0 | 0 | 1 | 3 | 9 |
| Top-10 items, same main cluster | 19,055 | 97.4 | 94 | 97 | 99 | 99 | 100 |
| Top-10 items, different main cluster | 825 | 91.1 | 80 | 90 | 95 | 98 | 99 |
| … of them sharing a second cluster | 552 | 95.3 | 90 | 94 | 97 | 98 | 99 |
| … of them sharing no cluster | 273 | 82.6 | 54 | 80 | 89 | 95 | 97 |

**Why the two groups' top-10 scores are close (97.4 and 91.1).** The score is a fixed
increasing function of cosine similarity and nothing else (AI-2.4). A profile reaches someone's
top 10 only by being among the 10 most similar of 9,999. So every top-10 item has a high
cosine, whatever its cluster, and gets a high score.
- On the tuning split, the median cosine of a different-cluster top-10 item is 0.922. That is
  higher than the median same-cluster pair (0.895).
- No increasing map can score those items below weaker same-field matches.
- Refitting on all pairs was tried on paper on 2026-11-15, aiming at most same-cluster pairs in
  75 to 100 and most different-cluster pairs below 60. The groups overlap: 10% of same-cluster
  pairs are below cosine 0.843, while 10% of different-cluster pairs are above 0.863. Meeting
  the aim at about 80% each would have pushed these top-10 items to about 99.

The map was therefore kept. Over all pairs it already separates the groups: different-cluster
pairs score 1 at the median and 9 at p90. Inside the top 10, the score shows how strong a near
neighbour is, not which field it is in. Two thirds of the "wrong" items are in a related field,
which justifies a high score. The 273 unrelated ones (82.6 on average) are the real cost: the
cosine cannot tell them apart from related near neighbours. The UI (17.2) should not present
90 as "same field".

**Why this is optimistic.** The sample profiles are made from templates: each cluster has its
own interests, skills, headline, bio, title and abstract templates (docs/decisions.md,
2026-11-15). Same-cluster profiles therefore share many exact words and phrases, which real
researchers do not. Expect clearly lower numbers on real profiles; this measures that the
pipeline works end to end, not real-world quality. The candidates also include the tuning
split, which only the score map was fitted on. The score map does not change the ranking, so
these numbers do not depend on it. The HNSW search is approximate, and its approximation is
part of what is measured.

## Recommendation speed (NFR-1), `make bench-recs`

**Method.**
- 200 random sample users (seed 17), after 10 warm-up calls that are not counted.
- Each call does the endpoint's whole uncached work through the recommendation usecase:
  1. the caller's fields and embedding stamp;
  2. the HNSW ranking in its own transaction, with `SET LOCAL` settings;
  3. the candidates' tags;
  4. the connection statuses through the social module.
- Calls run one after another, against the 10,000-profile development database.
- HTTP, the session check and Redis are not included; they add a few milliseconds.

**Results (2026-11-15):**

| Measure | Time |
| --- | ---: |
| Calls | 200 |
| p50 | 2.22 ms |
| p95 | 2.63 ms |
| max | 3.09 ms |
| mean | 2.25 ms |

**NFR-1 target:** p95 under 2 s with 10,000 profiles. **Met**, by a wide margin.

**Caveats.**
- The database was warm: every page was a cache hit.
- There was one client and no concurrent load.
- Sample users have 0 to 3 connections and no blocks, so the exclusions rarely make the
  iterative scan go past the first candidates.

A user with many connections near them in embedding space would make the scan read further,
up to pgvector's `hnsw.max_scan_tuples` (default 20,000). That is still one index scan.

**Query plan.** This is EXPLAIN (ANALYZE, BUFFERS) of the endpoint's own `RankPeople` text, for
one benchmark user, under the endpoint's settings. It shows:
- the HNSW index scan (`profile_embeddings_hnsw_idx`, `Order By: embedding <=> $1`);
- the anti-joins on `connections_live_pair_uq` and both `blocks` indexes;
- no sequential scan.

```text
Sort  (cost=445.03..445.06 rows=10 width=153) (actual time=0.703..0.706 rows=10 loops=1)
  Sort Key: cand.distance, cand.user_id
  Sort Method: quicksort  Memory: 26kB
  Buffers: shared hit=761
  CTE me
    ->  Index Scan using profile_embeddings_pkey on profile_embeddings  (cost=0.29..8.30 rows=1 width=89) (actual time=0.005..0.005 rows=1 loops=1)
          Index Cond: (user_id = '23f963b3-c642-489b-ba6b-8c615757c95b'::uuid)
          Buffers: shared hit=3
  CTE cand
    ->  Limit  (cost=312.20..353.31 rows=10 width=24) (actual time=0.577..0.670 rows=10 loops=1)
          Buffers: shared hit=731
          InitPlan 2 (returns $1)
            ->  CTE Scan on me  (cost=0.00..0.02 rows=1 width=32) (actual time=0.005..0.006 rows=1 loops=1)
                  Buffers: shared hit=3
          InitPlan 3 (returns $2)
            ->  CTE Scan on me me_1  (cost=0.00..0.02 rows=1 width=32) (actual time=0.001..0.001 rows=1 loops=1)
          ->  Nested Loop Anti Join  (cost=312.16..31178.11 rows=7508 width=24) (actual time=0.577..0.668 rows=10 loops=1)
                Join Filter: (((b.blocker_id = '23f963b3-c642-489b-ba6b-8c615757c95b'::uuid) AND (b.blocked_id = e.user_id)) OR ((b.blocker_id = e.user_id) AND (b.blocked_id = '23f963b3-c642-489b-ba6b-8c615757c95b'::uuid)))
                Buffers: shared hit=731
                ->  Nested Loop  (cost=303.76..29113.03 rows=7508 width=34) (actual time=0.555..0.594 rows=10 loops=1)
                      Buffers: shared hit=669
                      ->  Nested Loop Anti Join  (cost=303.47..25559.43 rows=7508 width=34) (actual time=0.550..0.567 rows=10 loops=1)
                            Buffers: shared hit=639
                            ->  Index Scan using profile_embeddings_hnsw_idx on profile_embeddings e  (cost=303.18..21473.25 rows=10010 width=34) (actual time=0.542..0.550 rows=10 loops=1)
                                  Order By: (embedding <=> $1)
                                  Filter: ((user_id <> '23f963b3-c642-489b-ba6b-8c615757c95b'::uuid) AND (model = $2))
                                  Rows Removed by Filter: 1
                                  Buffers: shared hit=619
                            ->  Index Scan using connections_live_pair_uq on connections c  (cost=0.29..0.41 rows=1 width=32) (actual time=0.001..0.001 rows=0 loops=10)
                                  Index Cond: ((LEAST(requester_id, receiver_id) = LEAST('23f963b3-c642-489b-ba6b-8c615757c95b'::uuid, e.user_id)) AND (GREATEST(requester_id, receiver_id) = GREATEST('23f963b3-c642-489b-ba6b-8c615757c95b'::uuid, e.user_id)))
                                  Buffers: shared hit=20
                      ->  Index Scan using users_pkey on users u  (cost=0.29..0.47 rows=1 width=16) (actual time=0.002..0.002 rows=1 loops=10)
                            Index Cond: (id = e.user_id)
                            Filter: ((email_verified_at IS NOT NULL) AND ((suspended_at IS NULL) OR ((suspended_until IS NOT NULL) AND (suspended_until <= now()))))
                            Buffers: shared hit=30
                ->  Materialize  (cost=8.40..19.17 rows=12 width=32) (actual time=0.000..0.001 rows=0 loops=10)
                      Buffers: shared hit=2
                      ->  Bitmap Heap Scan on blocks b  (cost=8.40..19.11 rows=12 width=32) (actual time=0.002..0.003 rows=0 loops=1)
                            Recheck Cond: ((blocker_id = '23f963b3-c642-489b-ba6b-8c615757c95b'::uuid) OR (blocked_id = '23f963b3-c642-489b-ba6b-8c615757c95b'::uuid))
                            Buffers: shared hit=2
                            ->  BitmapOr  (cost=8.40..8.40 rows=12 width=0) (actual time=0.002..0.002 rows=0 loops=1)
                                  Buffers: shared hit=2
                                  ->  Bitmap Index Scan on blocks_blocker_created_idx  (cost=0.00..4.20 rows=6 width=0) (actual time=0.001..0.001 rows=0 loops=1)
                                        Index Cond: (blocker_id = '23f963b3-c642-489b-ba6b-8c615757c95b'::uuid)
                                        Buffers: shared hit=1
                                  ->  Bitmap Index Scan on blocks_blocked_idx  (cost=0.00..4.20 rows=6 width=0) (actual time=0.001..0.001 rows=0 loops=1)
                                        Index Cond: (blocked_id = '23f963b3-c642-489b-ba6b-8c615757c95b'::uuid)
                                        Buffers: shared hit=1
  ->  Nested Loop  (cost=0.29..83.25 rows=10 width=153) (actual time=0.582..0.698 rows=10 loops=1)
        Buffers: shared hit=761
        ->  CTE Scan on cand  (cost=0.00..0.20 rows=10 width=24) (actual time=0.578..0.671 rows=10 loops=1)
              Buffers: shared hit=731
        ->  Index Scan using profiles_pkey on profiles p  (cost=0.29..8.30 rows=1 width=137) (actual time=0.002..0.002 rows=1 loops=10)
              Index Cond: (user_id = cand.user_id)
              Buffers: shared hit=30
Planning:
  Buffers: shared hit=14
Planning Time: 0.407 ms
Execution Time: 0.739 ms
```

## Semantic researcher search quality (AI-5.2), `make eval-search`

**Question.** Does `GET /search/people` in its default `auto` (hybrid) mode find researchers
whose profiles match a query in meaning when they share none of its words (AI-5.2)?

**Queries.** 40 plain-language queries in `backend/cmd/recseval/search_queries.json`, one
English and one Bangla for each of the 20 sample clusters, written by hand for slice 18 and
fixed before the first run. None was changed after seeing a result: that would be fitting
to the test split.

**No shared words, checked automatically.** `TestSearchQueriesShareNoWords`
(`cmd/recseval`) splits each query and its cluster's whole vocabulary into words. The
vocabulary covers every field of `cmd/seedsample/content/clusters.json`, English and Bangla,
and every word `common.json` adds to all profiles. The test fails when the two share a word.
- **Splitting:** lower case; letters, combining marks and digits form words, so a Bangla
  vowel sign stays in its word. Template placeholders such as `{interest}` are dropped.
- **Shared:** two words count as one when they are equal, when one starts the other and both
  have at least 4 letters ("computer" and "computers"), or when they have the same first 5
  letters ("electricity" and "electrical").
- **Exempt:** a fixed list of English and Bangla function words ("the", "of", "and", "ও",
  "এবং", "জন্য", ...), listed in the test.
- **Still allowed:** a shorter common stem ("imaging" against "images") is not caught; the
  queries were also read by hand for that.

**Candidates.** Only the 1,988 TEST split sample profiles. Each candidate query
(`StrongNameMatches`, `SearchPeoplePage`, `SemanticPeopleCandidates` and
`AnyVisibleNameContains`) is the product's own text (`profilepg.QueryText`). One join to
`sample_profiles` with `split = 'test'` is added after a fixed anchor line, and the command
stops if the anchor ever moves. Each query also runs under the endpoint's transaction
settings, and the lists are merged with the endpoint's `domain.FusePeople`. There are no
filters, and the viewer blocks nobody.

**Measure.** Precision@10 against the NFR-6 rule: relevant means the same `main_cluster`,
computed as hits in the top 10 divided by 10, so an empty or short list scores low. It is
reported for hybrid mode, keyword mode (`mode=keyword`, the slice 12 search alone) and, for
reference, the meaning side alone. Nothing is tuned: k = 60 is the original RRF paper's value,
and 50 candidates per side is the chunk's design.

**Results (2026-11-16), per query:**

| Cluster | Lang | Query | Hybrid P@10 | Keyword P@10 (results) | Meaning only P@10 |
| --- | --- | --- | ---: | ---: | ---: |
| medical_imaging | en | software that spots disease in pictures taken inside the body | 1.0 | 0.0 (0) | 1.0 |
| medical_imaging | bn | শরীরের ভেতরের ছবি দেখে অসুখ ধরতে পারে এমন যন্ত্র | 0.8 | 0.0 (0) | 0.8 |
| explainable_ai | en | why did the program choose that, told plainly to ordinary folks | 1.0 | 0.0 (0) | 1.0 |
| explainable_ai | bn | যন্ত্র কেন এমন রায় দিল সেটা সহজে বুঝিয়ে বলা | 0.9 | 0.0 (0) | 0.9 |
| bangla_nlp | en | getting software to grasp everyday talk and essays in our mother tongue | 1.0 | 0.0 (0) | 1.0 |
| bangla_nlp | bn | কম্পিউটারকে মাতৃভাষায় বই পড়তে ও বুঝতে শেখানো | 0.8 | 0.0 (0) | 0.8 |
| climate_modelling | en | how hot and wet summers get in coming decades | 1.0 | 0.0 (0) | 1.0 |
| climate_modelling | bn | আগামী দশকগুলোতে গরম আর বর্ষা কেমন বদলাবে | 1.0 | 0.0 (0) | 1.0 |
| public_health_epidemiology | en | tracking how sickness spreads among villages and towns | 1.0 | 0.0 (0) | 1.0 |
| public_health_epidemiology | bn | পাড়া মহল্লায় ছোঁয়াচে অসুখ কীভাবে ছড়ায় | 0.5 | 0.0 (0) | 0.5 |
| materials_science | en | inventing stronger lighter substances at atomic scale | 1.0 | 0.0 (0) | 1.0 |
| materials_science | bn | পরমাণু স্তরে মজবুত হালকা বস্তু বানানো | 0.9 | 0.0 (0) | 0.9 |
| robotics | en | machines that walk, grab objects and move around by themselves | 1.0 | 0.0 (0) | 1.0 |
| robotics | bn | নিজে নিজে চলাফেরা করা যান্ত্রিক মানুষ বানানো | 0.7 | 0.0 (0) | 0.7 |
| agri_remote_sensing | en | watching paddies from orbit to guess how much grain grows | 1.0 | 0.0 (0) | 1.0 |
| agri_remote_sensing | bn | মহাকাশ থেকে তোলা ছবিতে খেতের শস্য আন্দাজ | 0.9 | 0.0 (0) | 0.9 |
| renewable_energy | en | current from sunlight and breeze for hamlets | 0.7 | 0.0 (0) | 0.7 |
| renewable_energy | bn | সূর্যের আলো আর বাতাস থেকে কারেন্ট | 0.1 | 0.0 (0) | 0.1 |
| water_resources | en | alerting towns before creeks burst their banks, and wells safe to drink | 0.6 | 0.0 (0) | 0.6 |
| water_resources | bn | বানের খবর আগে জানানো ও নিরাপদ খাবার জল | 0.5 | 0.0 (0) | 0.5 |
| cybersecurity | en | stopping hackers from breaking into laptops and stealing passwords | 1.0 | 0.0 (0) | 1.0 |
| cybersecurity | bn | হ্যাকারদের হাত থেকে কম্পিউটার আর পাসওয়ার্ড রক্ষা | 0.9 | 0.0 (0) | 0.9 |
| genomics_bioinformatics | en | reading DNA letters by software to understand germs and heredity | 1.0 | 0.0 (0) | 1.0 |
| genomics_bioinformatics | bn | ডিএনএ পড়ে জীবাণু ও বংশগতি বোঝার কম্পিউটার কৌশল | 0.3 | 0.0 (0) | 0.3 |
| development_economics | en | lifting poor families out of want with tiny credit | 1.0 | 0.0 (0) | 1.0 |
| development_economics | bn | গরিব সংসারের রোজগার বাড়ানো ও ছোট ঋণ | 0.8 | 0.0 (0) | 0.8 |
| education_technology | en | websites that let pupils do lessons at home on tablets | 1.0 | 0.0 (0) | 1.0 |
| education_technology | bn | ছাত্রছাত্রীদের ঘরে বসে পড়াশোনার সফটওয়্যার | 0.8 | 0.0 (0) | 0.8 |
| structural_engineering | en | keeping homes and towers upright when the ground shakes | 1.0 | 0.0 (0) | 1.0 |
| structural_engineering | bn | মাটি কাঁপলে দালান যেন ভেঙে না পড়ে | 0.1 | 0.0 (0) | 0.1 |
| urban_transport | en | easing jams downtown so getting to offices takes less time | 0.3 | 0.0 (0) | 0.3 |
| urban_transport | bn | নগরের রাস্তায় গাড়ির জট কমিয়ে অফিসে তাড়াতাড়ি পৌঁছানো | 0.8 | 0.0 (0) | 0.8 |
| marine_fisheries | en | keeping the sea alive so nets can still bring in a good catch | 1.0 | 0.0 (0) | 1.0 |
| marine_fisheries | bn | সাগরের প্রাণ বাঁচিয়ে মাঝিমাল্লার জীবিকা টিকিয়ে রাখা | 0.7 | 0.0 (0) | 0.7 |
| wireless_networks | en | quicker phone reception and gadgets talking to each other | 0.9 | 0.0 (0) | 0.9 |
| wireless_networks | bn | মোবাইল ফোনের দ্রুত সংকেত ও যন্ত্রে যন্ত্রে কথা বলা | 0.2 | 0.0 (0) | 0.2 |
| drug_discovery | en | inventing pills and tablets that cure sick patients | 1.0 | 0.0 (0) | 1.0 |
| drug_discovery | bn | অসুখ সারানোর বড়ি ও ট্যাবলেট বানানো | 0.8 | 0.0 (0) | 0.8 |
| mental_health | en | comforting folks who feel sad, worried or hopeless | 0.9 | 0.0 (0) | 0.9 |
| mental_health | bn | দুশ্চিন্তা আর হতাশায় থাকা মানুষের পাশে দাঁড়ানো | 0.6 | 0.0 (0) | 0.6 |

**Summary:**

| Queries | Hybrid P@10 | Keyword P@10 | Meaning only P@10 |
| --- | ---: | ---: | ---: |
| en (20) | 0.920 | 0.000 | 0.920 |
| bn (20) | 0.655 | 0.000 | 0.655 |
| all (40) | 0.787 | 0.000 | 0.787 |

**Reading the results.**
- Keyword mode finds nothing for any of these queries: they share no word with the profiles
  by construction. Every hybrid hit is therefore a meaning-only match (AI-5.2), and hybrid
  equals the meaning side alone.
- English queries reach 0.92.
- Bangla queries reach 0.655, with a wide spread. Several are 0.8 to 1.0. The weak ones
  (renewable energy 0.1, structural engineering 0.1, wireless networks 0.2, genomics 0.3)
  use everyday words such as "কারেন্ট" (current), "মাটি কাঁপলে" (when the ground shakes)
  and "কথা বলা" (talking) that are far from the formal vocabulary of the 85% English sample
  text.
- English urban transport (0.3) and water resources (0.6) mix in neighbouring clusters
  (offices and commuting; towns and drinking water).
- As with NFR-6, the profiles come from templates, so real profiles will score differently.

**The Bangla gap (0.655 against 0.920).** This is the clearest finding. The most likely cause
is language bias in the multilingual model. A Bangla query sits close to Bangla-language
profiles of any field, so they crowd out the English profiles of the right cluster. Only
about 15% of the sample profiles have Bangla text (5% Bangla only, 10% mixed), so most of
the right answers are English. This is an explanation, not yet measured: no count was taken
of how many of the wrong top-10 hits have Bangla text.

Possible fixes to try later, each measured with this same `make eval-search` and the same
fixed queries:
- **A stronger multilingual embedding model.** This is a model change (AI-1.6): a full
  re-embed, a new score calibration (`make calibrate-recs`) and a re-run of every
  evaluation.
- **A Bangla query also embedded as an English translation.** The two candidate lists would
  be merged, for example as a third RRF list. This needs a translation step in the AI
  service and adds to the query time (NFR-1).
- **Per-language score normalisation.** Cosines would be rescaled per query language and
  profile language before ranking. Any scale must be fitted on the tuning split only.

This is an open item in `docs/decisions.md` (2026-11-16) for a later AI-quality slice. Nothing
changes in this chunk.

## Semantic researcher search speed (NFR-1), `make bench-search`

**Method.**
- 200 search texts: the 40 evaluation queries, then names of research interests that sample
  profiles hold, drawn with seed 17. Each searcher is a sample user drawn at random.
- 10 warm-up searches with other texts come first and are not counted.
- Each search does the usecase's whole hybrid work against the 10,000-profile development
  database:
  1. the query vector, embedded by the running AI service (CPU, in Docker);
  2. the strong name, keyword and meaning candidates in one read-only snapshot;
  3. fusion, the facets (both institution and interest), the page and its interests.
- Each text runs twice:
  - **cold:** the vector cache is empty, so every text is embedded;
  - **cached:** every vector is kept (an in-memory cache standing in for Redis).
- Searches run one after another.
- HTTP, the session check, the rate limit, Redis and the connection statuses are not
  included; together they add a few milliseconds.

**Results (2026-11-16):**

| Measure | Cold query vector | Cached query vector |
| --- | ---: | ---: |
| Searches | 200 | 200 |
| p50 | 52.06ms | 18.23ms |
| p95 | 62.99ms | 20.61ms |
| max | 71.94ms | 25.63ms |

**NFR-1 target:** p95 under 2 s with 10,000 profiles, the query embedding included. **Met**:
the cold p95 is about 3% of the budget. Embedding one short query takes about 35 ms on this CPU
(the difference of the two columns); the rest is the database work (the cached column).

**Caveats.**
- The database and the model were warm.
- One client and no concurrent load: the AI service embeds one request at a time, so many
  simultaneous cold searches queue there. The 1.5 s `SEARCH_AI_TIMEOUT` then falls back to
  keyword results (AI-5.4) rather than wait.
- No filters were set. A selective institution or interest filter makes the HNSW iterative
  scan read further, at most every embedding at this size (`hnsw.max_scan_tuples` is 20,000).

**Query plan.** This is EXPLAIN (ANALYZE, BUFFERS) of the endpoint's own
`SemanticPeopleCandidates` text, for the first query and a sample searcher, under the
endpoint's settings. The 768 numbers of the vector are left out. It shows:
- the HNSW index scan (`profile_embeddings_hnsw_idx`) ordered by `<=>`, with the model test
  as a filter;
- the anti-join on `blocks_blocked_idx`;
- primary-key lookups for the verification, suspension and profile checks;
- no sequential scan.

```text
Sort  (cost=845.44..845.57 rows=50 width=145) (actual time=1.491..1.495 rows=50 loops=1)
  Sort Key: cand.distance, cand.user_id
  Sort Method: quicksort  Memory: 32kB
  Buffers: shared hit=1421
  CTE cand
    ->  Limit  (cost=303.91..459.90 rows=50 width=24) (actual time=0.810..1.347 rows=50 loops=1)
          Buffers: shared hit=1271
          ->  Nested Loop  (cost=303.91..31518.79 rows=10005 width=24) (actual time=0.809..1.341 rows=50 loops=1)
                Buffers: shared hit=1271
                ->  Nested Loop  (cost=303.62..27908.51 rows=10005 width=50) (actual time=0.782..1.003 rows=50 loops=1)
                      Buffers: shared hit=1020
                      ->  Nested Loop Anti Join  (cost=303.34..23223.18 rows=10005 width=34) (actual time=0.775..0.862 rows=50 loops=1)
                            Buffers: shared hit=870
                            ->  Index Scan using profile_embeddings_hnsw_idx on profile_embeddings e  (cost=303.18..21448.22 rows=10011 width=34) (actual time=0.767..0.816 rows=50 loops=1)
                                  Order By: (embedding <=> '[768 numbers]'::vector)
                                  Filter: (model = 'intfloat/multilingual-e5-base@d128750597153bb5987e10b1c3493a34e5a4502a'::text)
                                  Buffers: shared hit=820
                            ->  Index Only Scan using blocks_blocked_idx on blocks x  (cost=0.15..0.18 rows=1 width=16) (actual time=0.001..0.001 rows=0 loops=50)
                                  Index Cond: ((blocked_id = '526b6ee2-7ee1-40d1-912b-e17d2d0731f3'::uuid) AND (blocker_id = e.user_id))
                                  Heap Fetches: 0
                                  Buffers: shared hit=50
                      ->  Index Scan using users_pkey on users u  (cost=0.29..0.47 rows=1 width=16) (actual time=0.002..0.002 rows=1 loops=50)
                            Index Cond: (id = e.user_id)
                            Filter: ((email_verified_at IS NOT NULL) AND (suspended_at IS NULL))
                            Buffers: shared hit=150
                ->  Index Only Scan using profiles_pkey on profiles p_1  (cost=0.29..0.36 rows=1 width=16) (actual time=0.002..0.002 rows=1 loops=50)
                      Index Cond: (user_id = u.id)
                      Heap Fetches: 0
                      Buffers: shared hit=101
  ->  Nested Loop  (cost=0.29..384.13 rows=50 width=145) (actual time=0.817..1.471 rows=50 loops=1)
        Buffers: shared hit=1421
        ->  CTE Scan on cand  (cost=0.00..1.00 rows=50 width=24) (actual time=0.812..1.361 rows=50 loops=1)
              Buffers: shared hit=1271
        ->  Index Scan using profiles_pkey on profiles p  (cost=0.29..7.66 rows=1 width=137) (actual time=0.002..0.002 rows=1 loops=50)
              Index Cond: (user_id = cand.user_id)
              Buffers: shared hit=150
Planning:
  Buffers: shared hit=38
Planning Time: 0.827 ms
Execution Time: 1.530 ms
```
