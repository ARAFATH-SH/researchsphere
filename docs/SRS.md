# ResearchSphere: Software Requirements Specification

Oct 7, 2026 · All Arafath Shojib · Version 1.0 (draft)

## 1. Introduction

This document states what ResearchSphere version 1.0 must do. It expands the 15 functional, 8 AI and 7 non-functional requirements of the [project proposal](https://claude.ai/code/artifact/57472ace-7221-4d81-a134-2861ec22c9cc) into numbered statements that can each be built and tested on their own.

### 1.1 Purpose

The document is the single reference for what to build. Its readers are the developer, any member who joins later, the course supervisor and whoever tests the system.

### 1.2 Product scope

ResearchSphere is a web platform for researchers and students. A user builds a research profile, finds collaborators and projects through AI matching, writes a paper with a team in a shared LaTeX editor, and gets a ranked list of venues for that paper. Section 8 lists what version 1.0 leaves out.

### 1.3 Definitions

| Term | Meaning in this document |
| --- | --- |
| Visitor | A person who is not logged in |
| Researcher | A registered, logged-in user. Students and faculty are both researchers |
| Administrator | A user with the admin role, who maintains venues and moderates content |
| Connection | A two-way link between two researchers, created when a request is accepted |
| Project | A research effort with a title, description, required skills, members and files |
| Workspace | The part of a project where members edit and compile its LaTeX files |
| Owner, editor, viewer | The three roles a member can hold inside one project |
| Venue | A conference, workshop or journal call that accepts paper submissions |
| Embedding | A list of numbers that represents the meaning of a text, produced by a language model |
| Semantic search | Search that ranks results by similarity of meaning using embeddings |
| Assistant | The AI feature in the editor that explains errors and drafts text on request |
| Sandbox | An isolated container in which user LaTeX code is compiled |
| ORCID iD | A public identifier for a researcher, in the form 0000-0000-0000-0000 |
| DOI | A public identifier for a publication |

### 1.4 Conventions

- **"Shall"** marks a requirement. Every requirement is one sentence that can be checked as true or false.
- **Identifiers:** FR, AI and NFR numbers match the proposal. A sub-requirement adds a second number, so FR-7.2 is the second statement under FR-7. NFR-8 onwards are new in this document.
- **Priority:**

| Priority | Meaning |
| --- | --- |
| Must | Version 1.0 is not complete without it |
| Should | Planned for version 1.0, and the first to be dropped if time runs short |
| Could | Built only if time remains |

- **Numeric limits** (lengths, time limits, page sizes) are starting values. Changing one is a change to this document.

### 1.5 References

1. [ResearchSphere: Project Proposal](https://claude.ai/code/artifact/57472ace-7221-4d81-a134-2861ec22c9cc), 5 October 2026.
2. IEEE Std 830-1998, Recommended Practice for Software Requirements Specifications.
3. ISO/IEC/IEEE 29148:2018, Systems and software engineering: Life cycle processes: Requirements engineering.

## 2. Overall Description

ResearchSphere is a new, standalone web system with three kinds of user and three roles inside each project.

### 2.1 Product perspective

The system has six parts: a web client, an API server, a sync server for real-time editing, a LaTeX compile service, an AI service and the data stores. The proposal's Section 5.2 describes them. The system depends on four outside services: OpenAlex and ORCID for publication metadata, a language model API for the assistant, and an email service for verification and reset links.

### 2.2 User classes

| User class | Description | Technical skill assumed |
| --- | --- | --- |
| Visitor | Sees the landing page and can register or log in | None |
| Researcher | Uses every feature outside administration | Can use a web application; LaTeX knowledge is not assumed |
| Administrator | Maintains venue data, removes content that breaks the rules, suspends accounts | Same as a researcher |

### 2.3 Permissions

The server enforces both tables (FR-3.2). An administrator has no extra rights inside a project and cannot open a workspace without being a member.

**Platform level**

| Action | Visitor | Researcher | Administrator |
| --- | --- | --- | --- |
| View the landing page, register, log in, reset a password | Yes | Yes | Yes |
| View profiles, search, feed, events and recommendations | No | Yes | Yes |
| Edit own profile, connect, post, like, comment | No | Yes | Yes |
| Create a project | No | Yes | Yes |
| Add, edit and remove venues | No | No | Yes |
| Remove any post or comment | No | No | Yes |
| Suspend a user account | No | No | Yes |
| Warn or restrict a user, and read their moderation history | No | No | Yes |
| Review reported conversations | No | No | Yes |

**Project level**

| Action | Non-member | Viewer | Editor | Owner |
| --- | --- | --- | --- | --- |
| View the project page (title, description, skills, members) | Yes | Yes | Yes | Yes |
| Request to join | Yes | No | No | No |
| Open the workspace, read files, view the PDF | No | Yes | Yes | Yes |
| See venue recommendations for the project | No | Yes | Yes | Yes |
| Edit files, upload figures | No | No | Yes | Yes |
| Compile, use the assistant, edit the abstract | No | No | Yes | Yes |
| Edit project details and status | No | No | No | Yes |
| Invite, decide join requests, change roles, remove members | No | No | No | Yes |
| Transfer ownership, delete the project | No | No | No | Yes |

### 2.4 Operating environment

- **Client:** current versions of Chrome and Firefox on a desktop or laptop. The workspace needs a screen at least 1280 pixels wide.
- **Server:** Linux with Docker. PostgreSQL 16 with the pgvector extension, and Redis.
- **Network:** HTTPS for all pages and API calls, secure WebSocket for real-time editing and messaging.

### 2.5 Constraints

- The project lasts 12 weeks and has one developer.
- AI models are pretrained. Nothing is trained or fine-tuned.
- External services are used within their free allowances.
- A file supports at most 6 simultaneous editors.
- The user interface is in English.

### 2.6 Assumptions and dependencies

- Every user has an email address they can read.
- OpenAlex and ORCID stay publicly available. If they are down, manual entry still works (FR-5.6).
- The language model API is reachable. If it is down, only the assistant stops working (NFR-10.2).
- Venue data can be obtained from a public call-for-papers listing under terms that allow it (open question in Section 8).

## 3. Functional Requirements

The 15 functional requirements of the proposal are grouped by module below. In every statement "the system shall" is the subject.

### 3.1 Accounts and access control

**FR-1 Registration**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-1.1 | let a visitor register with a full name, an email address and a password | Must |
| FR-1.2 | reject a registration when the email address is already registered or the password is shorter than 8 characters, and say which | Must |
| FR-1.3 | send a verification link to the email address; the link works once and expires after 24 hours | Must |
| FR-1.4 | refuse login until the email address is verified | Must |
| FR-1.5 | send a new verification link on request | Should |

**FR-2 Login, password and account deletion**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-2.1 | log a user in with email address and password, issuing an access token valid for 15 minutes and a refresh token valid for 30 days | Must |
| FR-2.2 | end the session on that device when the user logs out, so its refresh token no longer works | Must |
| FR-2.3 | send a password reset link on request; the link works once and expires after 1 hour | Must |
| FR-2.4 | give the same response to a reset request whether or not the email address is registered | Should |
| FR-2.5 | end all of a user's sessions when the password is reset or changed | Must |
| FR-2.6 | let a logged-in user change the password after entering the current one | Should |
| FR-2.7 | refuse further login attempts for an account for 15 minutes after 5 failed attempts within 15 minutes | Should |
| FR-2.8 | delete the account and its profile, posts, comments, likes, connections and embeddings when the user confirms with the password | Must |
| FR-2.9 | refuse account deletion while the user owns a project that has other members, and tell the user to transfer or delete that project first | Must |

**FR-3 Access control**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-3.1 | require a valid access token for every request except registration, verification, login and password reset | Must |
| FR-3.2 | check on the server, for every request, that the user's role permits the action according to the tables in Section 2.3 | Must |
| FR-3.3 | answer a request for an action the user may not perform with an error that reveals none of the protected content | Must |
| FR-3.4 | refuse login to a suspended account, saying why and, when the suspension is temporary, until when, only after the right password | Should |

### 3.2 Profile, publications and search

**FR-4 Research profile**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-4.1 | let a user create and edit a profile with full name, headline, bio, institution, department, job title, country and city | Must |
| FR-4.2 | let a user add and remove research interests and skills as tags, treating tags that differ only in letter case as the same tag | Must |
| FR-4.3 | suggest existing tags while the user types a tag | Should |
| FR-4.4 | let a user add, edit and remove education entries with institution, degree, field, years, grade and description | Must |
| FR-4.5 | show any user's profile to a logged-in user, with interests, skills, education, confirmed publications and the connection status between the two | Must |
| FR-4.6 | let a user upload a profile photo of up to 5 MB (JPEG, PNG or WebP), stored as a 400x400 JPEG, and a cover image of up to 8 MB, stored as a 1584x396 JPEG | Must |
| FR-4.7 | let a user add contact info (phone, birthday as day and month without year, and up to 5 website links) and choose for each item, and for the account email, who can see it: only me or all logged-in users | Should |
| FR-4.8 | let a user add, edit and remove work experience entries with title, organization, employment type, location, location type, start and end month, current role, and description | Must |
| FR-4.9 | let a user list the languages they know, each with a proficiency level | Should |

**FR-5 Publications**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-5.1 | let a user add a publication by hand with title, year, venue, abstract and link | Must |
| FR-5.2 | fetch title, abstract, venue and year for a DOI the user enters, and show them for confirmation before saving | Must |
| FR-5.3 | list the public works of an ORCID iD the user enters, and import only the ones the user selects | Must |
| FR-5.4 | store a publication with a given DOI once, and link a second author to the existing record | Must |
| FR-5.5 | show on a profile only the publications that user has confirmed | Must |
| FR-5.6 | show a clear message and keep manual entry available when an outside metadata service fails | Should |
| FR-5.7 | let a user remove a publication from the profile | Must |

**FR-6 Researcher search**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-6.1 | return researchers whose name, institution or research interests match the search text, 20 per page | Must |
| FR-6.2 | match a name that has a small spelling mistake | Should |
| FR-6.3 | let the user narrow results by institution and by research interest | Should |

### 3.3 Connections, feed and notifications

**FR-7 Connections**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-7.1 | let a user send a connection request to another user | Must |
| FR-7.2 | refuse a request to oneself, and a request to a user with whom a pending request or a connection already exists in either direction | Must |
| FR-7.3 | let the receiver accept or reject a request, and let the sender cancel it while it is pending | Must |
| FR-7.4 | list a user's connections, received requests and sent requests | Must |
| FR-7.5 | let either user remove an existing connection | Should |
| FR-7.6 | allow a new request after an earlier one was rejected | Should |

**FR-8 Feed**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-8.1 | let a user publish a text post of up to 5,000 characters, optionally linked to one of the user's own publications or projects | Must |
| FR-8.2 | show a feed of posts by the user and the user's connections, newest first, 20 per page | Must |
| FR-8.3 | let a user like a post once and remove that like, and show the number of likes | Must |
| FR-8.4 | let a user comment on a post with up to 2,000 characters and delete the user's own comment | Must |
| FR-8.5 | let an author edit and delete the author's own post | Must |
| FR-8.6 | show a user's posts on that user's profile | Should |

**FR-9 Notifications**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-9.1 | create a notification for the receiver when a connection request arrives or is accepted | Must |
| FR-9.2 | create a notification when a project join request or invitation arrives, and when one is accepted or rejected | Must |
| FR-9.3 | create a notification for a post's author when another user comments on it | Must |
| FR-9.4 | show the number of unread notifications on every page and refresh it at least every 60 seconds | Should |
| FR-9.5 | list notifications newest first, each linking to the item it is about | Must |
| FR-9.6 | let a user mark one notification or all notifications as read | Must |
| FR-9.7 | create a notification for a post's author when another user likes it | Could |

### 3.4 Projects and workspace

**FR-10 Research projects**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-10.1 | let a user create a project with a title, a description, required skills and an optional paper abstract, and make that user its owner | Must |
| FR-10.2 | let the owner edit the project's details and set its status to open, closed or archived | Must |
| FR-10.3 | return projects whose title, description or required skills match the search text, 20 per page, showing only open projects | Must |
| FR-10.4 | create a starting main.tex file in every new project | Should |
| FR-10.5 | let the owner transfer ownership to another member | Should |
| FR-10.6 | let the owner delete the project and all its files after confirming | Should |

**FR-11 Membership**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-11.1 | let a user request to join an open project with a short message, allowing one pending request per user per project | Must |
| FR-11.2 | let the owner accept a request and assign the role editor or viewer, or reject it | Must |
| FR-11.3 | let the owner invite a user, and let that user accept or reject the invitation | Should |
| FR-11.4 | let the owner change a member's role and remove a member | Must |
| FR-11.5 | let a member other than the owner leave the project | Must |
| FR-11.6 | list a user's own projects with the user's role in each | Must |

**FR-12 Collaborative LaTeX editor**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-12.1 | let an editor create, rename and delete files and folders in the workspace | Must |
| FR-12.2 | let an editor upload figure files (PNG, JPG, PDF) of up to 5 MB each | Must |
| FR-12.3 | provide a LaTeX editor with syntax highlighting, line numbers, undo, redo and find | Must |
| FR-12.4 | let up to 6 members edit the same file at the same time and merge their changes so that no member's text is lost | Must |
| FR-12.5 | show each active member's cursor and name in a distinct colour | Must |
| FR-12.6 | save changes automatically, with no save action from the user | Must |
| FR-12.7 | check a member's identity and role before accepting a real-time connection, and discard edits sent by a viewer | Must |
| FR-12.8 | open the file read-only for a seventh member and say why | Should |
| FR-12.9 | merge the edits a member made while disconnected when the connection returns | Should |

**FR-13 Compile**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-13.1 | compile the project's main file with pdfLaTeX and BibTeX when an editor asks, and show the PDF beside the editor | Must |
| FR-13.2 | show the compile log and a list of errors, each with its file and line number | Must |
| FR-13.3 | move the cursor to the file and line of an error the user selects | Should |
| FR-13.4 | run one compile per project at a time and tell a member who asks during a compile that one is already running | Must |
| FR-13.5 | stop a compile that runs longer than 60 seconds and report a timeout | Must |
| FR-13.6 | let a member download the latest PDF | Must |
| FR-13.7 | let a member download the project's files as a ZIP archive | Could |

### 3.5 Events and administration

**FR-14 Events**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-14.1 | list venues with name, type, field, location, dates, submission deadline and link, ordered by nearest deadline | Must |
| FR-14.2 | hide venues whose submission deadline has passed unless the user asks to see them | Must |
| FR-14.3 | let the user filter venues by type, by field and by a deadline date range | Must |
| FR-14.4 | show a venue's full scope text on its own page | Must |
| FR-14.5 | import venues from a public call-for-papers listing, updating an entry that was imported before in place of adding a duplicate | Should |

**FR-15 Administration**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-15.1 | let an administrator add, edit and remove venues | Must |
| FR-15.2 | let an administrator remove a post or a comment with a recorded reason, after which no user sees it | Must |
| FR-15.3 | let an administrator list users, and suspend and restore an account, each with a reason shown to the user | Should |
| FR-15.4 | show an administrator how many compile jobs and embedding jobs succeeded and failed in the last 24 hours | Could |
| FR-15.5 | let an administrator warn a user with a reason; the user gets a notification and an email, and sees the warning until acknowledging it | Should |
| FR-15.6 | let an administrator stop a user sending messages and message requests for 1 to 90 days; it ends by itself or when an administrator lifts it, and nothing else changes for the user | Should |
| FR-15.7 | let a suspension be permanent or last 1 to 365 days, after which it ends by itself; suspending ends every session of the account, and the writes named in docs/decisions.md are refused at once | Should |
| FR-15.8 | record every moderation action with the administrator, user, action, reason, internal note, report and time, in the transaction of the action; email the user the reason and never the note; and, when the action names a report, resolve it as action taken | Should |
| FR-15.9 | show an administrator a user's moderation history, newest first, with linked reports | Should |
| FR-15.10 | refuse every moderation action on an administrator, oneself included | Should |

### 3.6 Messaging

Messaging lets researchers reach each other directly. Connected users chat at once; anyone else first sends a message request.

**FR-16 Messaging**

| ID | The system shall | Priority |
| --- | --- | --- |
| FR-16.1 | let two connected users start a one-to-one conversation and exchange text messages that reach the other user within 1 second while both are online | Must |
| FR-16.2 | let a user send a message request to someone they are not connected to; nothing more can be sent until the receiver accepts, and a declined request closes the conversation | Must |
| FR-16.3 | show the unread count per conversation and in total, and show the sender when a message has been seen | Must |
| FR-16.4 | show when the other person is typing and whether they are online | Should |
| FR-16.5 | let a user edit or delete their own message; an edited message is labeled as edited | Should |
| FR-16.6 | let a user create a group conversation of up to 50 members, name it, and let its creator add and remove members | Should |
| FR-16.7 | let a user attach files (PDF, images and office documents) of up to 25 MB to a message; each file is checked by type and scanned for malware before others can download it | Should |
| FR-16.8 | let a user search the text of their own conversations | Should |
| FR-16.9 | let a user block another user, after which neither can message the other, and report a conversation or message to administrators | Must |
| FR-16.10 | limit how many messages and message requests a user can send per minute and per day | Must |
| FR-16.11 | email a digest of messages left unread for 24 hours; the user can turn the digest off | Could |

## 4. AI Requirements

The 8 AI requirements of the proposal are expanded below. Recommendation, search and venue ranking all use the same embeddings, so AI-1 is built first.

**AI-1 Embeddings**

| ID | The system shall | Priority |
| --- | --- | --- |
| AI-1.1 | create a profile embedding from the user's interests, skills, headline, bio and the titles and abstracts of confirmed publications | Must |
| AI-1.2 | create a project embedding from its title, description, required skills and abstract | Must |
| AI-1.3 | create a venue embedding from its name and scope, and a post embedding from its text | Must |
| AI-1.4 | update an embedding within 60 seconds of a change to its source text, and skip the update when the text has not changed | Must |
| AI-1.5 | create embeddings in the background, so that saving a profile, project, post or venue does not wait for the model | Must |
| AI-1.6 | record which model produced each embedding, so that all embeddings can be recreated when the model changes | Should |

**AI-2 Collaborator recommendation**

| ID | The system shall | Priority |
| --- | --- | --- |
| AI-2.1 | show a user the 10 researchers whose profile embeddings are most similar to the user's own, leaving out the user, existing connections and users with a pending request | Must |
| AI-2.2 | show with each recommendation a similarity score as a percentage and up to 3 interests or skills the two users share | Must |
| AI-2.3 | show a project's owner the 10 researchers whose profiles are most similar to the project, leaving out current members | Must |
| AI-2.4 | rank without using citation counts, publication counts or connection counts | Must |

**AI-3 Project recommendation**

| ID | The system shall | Priority |
| --- | --- | --- |
| AI-3.1 | show a user the 10 open projects most similar to the user's profile, leaving out projects the user belongs to or has asked to join | Must |

**AI-4 Users without publications**

| ID | The system shall | Priority |
| --- | --- | --- |
| AI-4.1 | produce recommendations for a user who has at least one interest or skill and no publications | Must |
| AI-4.2 | ask a user whose profile has no interests, skills or bio to add some, in place of showing an empty list | Must |

**AI-5 Semantic search**

| ID | The system shall | Priority |
| --- | --- | --- |
| AI-5.1 | accept a plain-language query of up to 300 characters and return researchers and projects ranked by similarity of meaning | Must |
| AI-5.2 | include results that match in meaning but contain none of the query's words | Must |
| AI-5.3 | return posts for the same query | Should |
| AI-5.4 | fall back to keyword search (FR-6, FR-10.3) and say so when the AI service does not respond | Should |

**AI-6 Venue recommendation**

| ID | The system shall | Priority |
| --- | --- | --- |
| AI-6.1 | show a project's members the 5 venues whose scope is most similar to the project's title and abstract, counting only venues whose submission deadline has not passed | Must |
| AI-6.2 | show each recommended venue's submission deadline and similarity score | Must |
| AI-6.3 | ask for an abstract in place of showing recommendations when the project has none | Must |
| AI-6.4 | state beside the list that a scope match says nothing about a venue's quality | Must |

**AI-7 Error explanation**

| ID | The system shall | Priority |
| --- | --- | --- |
| AI-7.1 | offer, after a failed compile, to explain the first error, and return a plain-language explanation and a suggested fix within 15 seconds | Must |
| AI-7.2 | send to the language model only the error message, the file name and the 20 lines around the error | Must |
| AI-7.3 | apply a suggested fix only when the user chooses to, as an edit the user can undo | Should |

**AI-8 Drafting and rules for the whole assistant**

| ID | The system shall | Priority |
| --- | --- | --- |
| AI-8.1 | draft a title, an abstract or a list of keywords from the project's document when an editor asks | Must |
| AI-8.2 | show every draft as a suggestion that the editor accepts, changes or rejects, and never write it into the document unasked | Must |
| AI-8.3 | tell the user, before the first use of the assistant, that the text is sent to an outside language model, and record the user's agreement | Must |
| AI-8.4 | label assistant output as AI-generated | Must |
| AI-8.5 | limit each user to 30 assistant requests per day | Should |
| AI-8.6 | record the kind of each assistant request and whether its suggestion was accepted, without storing the suggestion text | Should |

## 5. External Interface Requirements

The system has one user interface (the web client), four outside services and two communication channels.

### 5.1 User interface

| Page | What the user does there | Requirements |
| --- | --- | --- |
| Landing, Register, Login, Reset | Creates an account and signs in | FR-1, FR-2 |
| Home | Reads the feed and sees recommended collaborators and projects | FR-8, AI-2, AI-3 |
| Search | Searches researchers, projects and posts by keyword or meaning | FR-6, FR-10.3, AI-5 |
| Profile | Views a profile; edits their own, including publications | FR-4, FR-5 |
| Connections | Handles requests and lists connections | FR-7 |
| Notifications | Reads and clears notifications | FR-9 |
| Projects | Lists own projects, creates a project | FR-10, FR-11.6 |
| Project page | Sees details and members, handles requests, sees recommended researchers and venues | FR-10, FR-11, AI-2.3, AI-6 |
| Workspace | Edits files, compiles, reads the PDF, uses the assistant | FR-12, FR-13, AI-7, AI-8 |
| Events | Browses and filters venues | FR-14 |
| Admin | Maintains venues, removes content, manages users | FR-15 |

Every page except the workspace shall be usable at a width of 768 pixels.

### 5.2 Outside services

| Service | Used for | If it fails |
| --- | --- | --- |
| OpenAlex API | Publication metadata for a DOI (FR-5.2) | Manual entry stays available |
| ORCID public API | The list of works for an ORCID iD (FR-5.3) | Manual entry stays available |
| Language model API | Error explanation and drafts (AI-7, AI-8) | The assistant shows an error; editing and compiling continue |
| Email service | Verification and password reset links (FR-1.3, FR-2.3) | The user can ask for the link again |

### 5.3 Communication

- The client and the API server exchange JSON over HTTPS, under the path /api/v1.
- The API is described in one OpenAPI 3 document, which is the contract between client and server.
- Every error response has the same shape: a short code and a readable message. A request that breaks the API contract also names the field or parameter at fault, without repeating the value sent.
- Lists are paginated with a page size the server caps at 50.
- Real-time editing uses a secure WebSocket connection to the sync server, opened with the user's access token.
- Real-time messaging uses a secure WebSocket connection to the API server, opened with the user's access token.

## 6. Non-Functional Requirements

NFR-1 to NFR-7 come from the proposal. NFR-8 to NFR-14 are added here.

| ID | Category | Requirement | Priority |
| --- | --- | --- | --- |
| NFR-1 | Performance | Recommendation and semantic search results shall be returned within 2 seconds with 10,000 profiles in the database | Must |
| NFR-2 | Performance | An edit by one editor shall appear for the other editors within 1 second on a normal broadband connection | Must |
| NFR-3 | Performance | A 10-page document shall compile within 20 seconds | Must |
| NFR-4 | Security | Each compile shall run in its own container with no network access, shell escape disabled, access to that project's files only, and limits of 60 seconds, 1 CPU and 1 GB of memory | Must |
| NFR-5 | Security | Passwords shall be stored as salted hashes, tokens shall be stored as hashes, and all traffic shall use HTTPS | Must |
| NFR-6 | AI quality | The system's recall at 10 for collaborator matching and top-5 accuracy for venue ranking shall be measured on a held-out test set and reported | Must |
| NFR-7 | Testing and compatibility | Automated tests shall run on every push, and the client shall work on current Chrome and Firefox | Must |
| NFR-8.1 | Security | Every input shall be validated on the server, and every database query shall use parameters | Must |
| NFR-8.2 | Security | Text entered by users shall be displayed as text and never run as code in another user's browser | Must |
| NFR-8.3 | Security | Registration, login and password reset shall each accept at most 10 requests per minute from one address | Should |
| NFR-9.1 | Privacy | The system shall store only data the user enters and public publication metadata | Must |
| NFR-9.2 | Privacy | Project text shall be sent to the language model only when a user asks the assistant for help | Must |
| NFR-10.1 | Reliability | A document's content shall be saved to the database within 5 seconds of the last edit, and survive a restart of the sync server | Must |
| NFR-10.2 | Reliability | A failure of the AI service, the compile service or an outside service shall leave the remaining features working | Must |
| NFR-11.1 | Maintainability | Database changes shall be made only through numbered migrations that can be applied and undone | Must |
| NFR-11.2 | Maintainability | The API server's business logic shall have automated tests covering at least 70% of its lines | Should |
| NFR-12 | Capacity | The deployment shall support 10 files being edited at once, each by 6 editors | Should |
| NFR-13 | Security | Uploaded files shall be kept in private object storage, served only through short-lived presigned URLs after a permission check, and encrypted at rest | Must |
| NFR-14 | Security | Every read and write of a conversation or message shall check on the server that the user is a member; anyone else, an administrator included, shall get the same "not found" answer as for a conversation that does not exist. The one exception: an administrator may read a conversation only through a report on it, only the messages up to the moment of that report, and every such read shall be recorded with the administrator, the report and the time | Must |

## 7. Data Requirements

The database holds the 32 kinds of record below. The rules below are enforced by the database itself, so that a fault in the application cannot break them.

| Entity | Main attributes | Rules |
| --- | --- | --- |
| User | Email, password hash, role, verified time, suspended time, suspension end and reason, AI consent time | Email is unique, ignoring letter case; a suspension end or reason only on a suspended account; a suspension ends after it starts |
| Session | User, refresh token hash, expiry, revoked time, hashes of earlier (retired) refresh tokens, kept to detect reuse | One per logged-in device; the token hash and every retired hash are unique |
| One-time token | User, purpose (verification or reset), token hash, expiry, used time | The token hash is unique; works once |
| Profile | Name, headline, bio, institution, department, job title, country, ORCID iD | One per user; ORCID iD is unique and well formed |
| Interest, Skill | Name | Name is unique, ignoring letter case; shared by all users and projects |
| Education | Institution, degree, field, start and end year | End year is not before start year |
| Contact info | Phone, birth month and day, who can see the email, phone and birthday | One per user; phone in E.164 format; month and day both set or both empty and a real date (29 February allowed); each visibility is only me or all logged-in users |
| Profile link | Kind (website, Google Scholar, ResearchGate, LinkedIn, GitHub, other), URL | At most 5 per user; URL uses https and has at most 300 characters |
| Publication | DOI, title, abstract, venue name, year, link, source | DOI is unique; linked to each author with a confirmed time |
| Connection | Requester, receiver, status, times | No self-connection; one pending or accepted connection per pair, in either direction |
| Post, Like, Comment | Author, text, linked publication or project, removal time and reason | One like per user per post |
| Project | Title, description, abstract, status, main file | Deleting a project deletes its files, members and requests |
| Membership | Project, user, role | One role per user per project; exactly one owner per project |
| Join request | Project, user, kind (request or invitation), status, message | One pending request per user per project |
| File | Project, path, text content, editing state, storage key, size | Path is unique within a project and cannot point outside it |
| Compile job | Project, requester, status, PDF key, log, times | Belongs to one project |
| Venue | Name, type, scope, field, location, dates, deadline, source | An imported venue is unique by its source and source identifier |
| Embedding | Owner (profile, project, post or venue), model, text hash, vector | One per owner; deleted with its owner |
| Notification | Receiver, type, actor, item, read time | Deleted with its receiver; a system type has no actor, every other type has one |
| Assistant request | User, project, kind, accepted or not | The suggestion text is not stored |
| Conversation | Kind (direct or group), state (active, request or declined), who requested it, the pair of users, last message time | One direct conversation per pair of users; a request names who requested it |
| Conversation member | Conversation, user, joined time, read marker and its time | One row per user per conversation; the read marker is a message of the same conversation |
| Message | Conversation, sender, text, client ID, sent, edited and deleted times | 1 to 5,000 characters, trimmed; a client ID is used once per sender; a deleted message keeps its place with no text; edited and deleted after it was sent |
| Message version | Message, text, shown from, shown until, ended by (edit or deletion) | One per replaced text, at most one deletion per message; read only through an administrator's report (NFR-14); deleted with the message |
| Block | Blocker, blocked user, time | One per direction; no self-block; deleted with either user |
| Message report | Conversation, reporter, reported user, message, reason, details, time, resolution (time, administrator, outcome, note) | A reported message belongs to the conversation; deleted with the conversation |
| Report view | Report, administrator, time | One per administrator's read; holds no message text and outlives the report |
| Account warning | User, reason, time, acknowledged time | Reason of 10 to 1,000 characters; deleted with the user |
| Account restriction | User, kind, start, end, lifted time, who lifted it | Ends after it starts; deleted with the user |
| Moderation action | User, administrator, action, reason, internal note, report, warning or restriction, end, time | One per action, never changed; deleted with the user, keeps its row without the name when the administrator's account is deleted, and outlives its report |

**Retention**

- Deleting an account removes the user's personal data at once (FR-2.8). Publications shared with a co-author stay for that co-author.
- Deleting an account deletes the user's direct conversations with all their messages, also the other member's copy.
- The compile log and PDF are kept for the latest compile of each project only.
- Earlier versions of edited messages and the text of deleted ones are deleted 30 days after they
  were replaced. A version of a conversation that was reported while or after that text was shown
  is kept until 30 days after the report is resolved (purge job: slice 29A).

## 8. Out of Scope and Open Questions

### 8.1 Out of scope for version 1.0

- Research groups and communities.
- Dataset hosting and file sharing outside a project's figures.
- Sharing or reposting another user's post.
- Email notifications other than verification, password reset and moderation actions (FR-15.8).
- Login through Google or ORCID.
- Comments, version history and templates in the editor.
- Hosting full-text papers, peer review, submission to venues and plagiarism detection.
- Recommendations that learn from user feedback.
- Mobile applications and payments.

### 8.2 Open questions

Each question blocks the requirement named beside it and needs an answer before that requirement is built.

- [ ] Which language model API will the assistant use, and what does it cost at 30 requests per user per day? (AI-7, AI-8)
- [ ] Which email service will send verification and reset links? (FR-1.3, FR-2.3)
- [ ] Which call-for-papers listing will venues come from, and do its terms allow automated import? (FR-14.5)
- [x] Where will figures and PDFs be stored: on the server's disk or in object storage? (FR-12.2, FR-13.1) Answer: S3-compatible object storage, MinIO locally (docs/decisions.md, 2026-10-18 and 2026-10-20).
- [ ] What server will host the deployment, and does it have enough memory to run the embedding model and TeX Live together? (NFR-1, NFR-3)
- [ ] Which public data will the AI evaluation use as its test set? (NFR-6)
