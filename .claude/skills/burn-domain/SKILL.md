---
name: burn-domain
description: How regional burns work and the words burners use — events and their lifecycle (land prep, build, gate, strike, inventory), departments and positions, leadership, qualifications (training, ICS certificates, age), volunteering, who may see what, and how regionals differ. Load before designing, naming, reviewing or explaining anything a burner will see or use, and update it whenever the owner explains how burns work.
---

# How burns work

bms is built for people who run regional burns. Use their words and match how they actually
work. This file is the project's memory of that domain.

**Keep it current.** Whenever the owner (or another burner) explains how burns work, add it
here in the same session, in the right section, with the date. Keep it generic: name no
specific regional, person or vendor. Anything specific to one regional goes in
`plan/private/regional-notes.md` (gitignored) instead. Mark what came from the owner
**(owner, date)**; general background without that mark should be checked with the owner
before a design depends on it.

## Words

| Word | Meaning | Don't say |
|---|---|---|
| **Regional** | an organization that runs a burn in a region, usually under a nonprofit or LLC | tenant, customer |
| **Event** | a gathering the regional puts on, with its dates: usually a burn, but also, for example, an after-burn decompression party. Work such as land clearing is **not** an event (owner, 2026-10-16) | edition (owner: burners won't understand it) |
| **Decompression (decomp)** | a party some time after the burn; an event in its own right (owner, 2026-10-16) | |
| **Participant** | anyone who attends | user (in the UI), attendee |
| **Playa name** | the name a person goes by at burns; how people know each other | nickname, display name (in the UI) |
| **Department** | a standing part of the regional (Rangers, Gate, Greeters, Medical, DPW, Placement, Art...) | team (ambiguous) |
| **Position** | a role within a department, often with requirements; for example Khaki and R12 on a Ranger team (owner, 2026-10-16) | sub-team |
| **Lead** | a person leading a department | manager |
| **Shift** | a block of volunteer time in a position | |
| **Theme camp** | a group camping together and offering something to the event | |
| **MOOP** | matter out of place: anything left on the land that shouldn't be | |
| **Strike** | taking everything down after the event | |
| **LLC / board** | the legal organization's members or directors | |

## Events and the year around them

- An event has a **start and an end, each a date and time** (owner, 2026-10-16). Inside it
  there are further scheduled dates and times, such as **gate hours** (when the gate is open
  to arrivals) and the **temple burn** (owner, 2026-10-16). Some of these are windows, some
  a single moment.
- An event has its dates, but the work runs far beyond them (owner, 2026-10-16): **land
  prep, build, inventory** before; **strike**, clean-up and inventory after. This work is
  made of activities related to an event, not events themselves. **Training** is probably
  the same: an event-related activity (owner, 2026-10-16, tentative). Some work
  belongs to a department and no event at all (for example, winter inventory).
- Each of these activities may be **open to anyone**, **invitation only**, or **only for
  certain positions** (owner, 2026-10-16).
- After the event, departments write after-burn reports and carry lessons into the next one.

## Departments, positions and leadership

- **Departments are standing** and not tied to any event; an unused department simply has
  nothing for a given event. Departments get **archived** when no longer needed, not deleted
  (owner, 2026-10-16).
- **Departments have no sub-teams** as a rule. Divisions within a department are **positions**
  (owner, 2026-10-16). (Burning Man itself has sub-teams, but even those can be modeled as
  positions.)
- **Each department has its own admins and manages itself** and its people (owner,
  2026-10-16). Site-wide staff can archive a department and set or reset its leadership, so
  a department whose leads have left isn't stuck.
- **Leadership terms differ by regional** (owner, 2026-10-16): at some, leads must re-apply
  to their positions every year and aren't reinstated automatically; at others, leads stay
  until they step down. Don't build one rule in.

## Qualifications and requirements

- Positions and shifts have **requirements** (owner, 2026-10-16): training completed,
  certificates, being over 18. Examples on a Ranger team: **Khaki** requires ICS-100 and
  training; **R12** requires ICS-200 and training.
- **ICS-100, ICS-200**: Incident Command System courses (FEMA, United States), common
  requirements for safety and emergency positions. Certificates can be recorded with the
  date earned; whether they expire depends on the regional.
- How bms holds and checks these is Q-QUAL.

## Who may see what

- **Separation of duties matters** (owner, 2026-10-16). No everyday role should see
  everything. Regionals set their own rules about who may never see what; for example, at
  some burns, board or LLC members must never review incident reports.
- A break-glass superuser exists for development and emergencies, never for normal work.
- People care about who sees their personal data. Regionals differ on how much to tell a
  person about views of their data: nothing, that it was viewed, which fields, or the
  viewer's title, never the viewer's name (owner, 2026-10-16; D22).
- Incident reports, medical and safety information are the most sensitive data a regional
  holds (`privacy-data` skill).

## Between regionals

- Groups of regionals in one area may agree to recognize each other's volunteer credit, for
  example a regional Ranger council whose member burns count Ranger shifts at any of them
  (owner, 2026-10-16; D18, Q-TRUST).

## How regionals differ

When a rule above says "differs by regional", it becomes a setting or data in bms, never
code. Known differences so far: leadership terms, who may see incident reports, how much a
person sees about views of their data, whether legal name and phone are required,
open or invitation-only sign-up, terminology.
