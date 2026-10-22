---
name: burn-domain
description: How regional burns work and the words burners use — events and their lifecycle (land prep, build, gate, strike, inventory), departments and positions, leadership, qualifications (training, ICS certificates, age), volunteering and shifts, incidents and must-reports, radios, conduct and complaints, pre-event registration, who may see what, and how regionals differ. Load before designing, naming, reviewing or explaining anything a burner will see or use, and update it whenever the owner explains how burns work.
---

# How burns work

bms is built for people who run regional burns. Use their words and match how they actually
work. This file is the project's memory of that domain.

**Keep it current.** Whenever the owner (or another burner) explains how burns work, add it
here in the same session, in the right section, with the date. Keep it generic: name no
specific regional, person or vendor. Anything specific to one regional goes in
`plan/private/regional-notes.md` (gitignored) instead. Mark what came from the owner
**(owner, date)**. Points marked **(manuals)** come from published volunteer-team manuals
and trainings the owner shared (2026-10-17): Ranger, perimeter, peer-support and
temple-guardian teams at Burning Man and at regionals. They describe how teams commonly
work; check with the owner before a design depends on one. General background with neither
mark should be checked too.

## Words

| Word | Meaning | Don't say |
|---|---|---|
| **Regional** | an organization that runs a burn in a region, usually under a nonprofit or LLC | tenant, customer |
| **Event** | a gathering the regional puts on, with its dates: usually a burn, but also, for example, an after-burn decompression party. Work such as land clearing is **not** an event (owner, 2026-10-16) | edition (owner: burners won't understand it) |
| **Decompression (decomp)** | a party some time after the burn; an event in its own right (owner, 2026-10-16) | |
| **Participant** | anyone who attends | user (in the UI), attendee |
| **Playa name** | the name a person goes by at burns; how people know each other | burn name (what burners often call it; the UI says display name), nickname |
| **Department** | a standing part of the regional (Rangers, Gate, Greeters, Medical, DPW, Placement, Art...) | team (ambiguous) |
| **Position** | a role within a department, often with requirements; for example Khaki and R12 on a Ranger team (owner, 2026-10-16) | sub-team |
| **Lead** | a person leading a department | manager |
| **Volunteer coordinator** | the role that runs volunteer sign-up and the roster across departments; usually the one role besides a few others who sees volunteers' legal names (owner, 2026-10-20) | |
| **Shift** | a block of volunteer time in a position | |
| **Shift lead / supervisor** | runs one shift of a department: briefs, pairs and dispatches volunteers, keeps the shift log; among Rangers this is often **Khaki**, which is also the shift command's radio call sign (manuals) | manager |
| **Dirt Ranger** | a Ranger on foot or bike patrol, always in a pair (manuals) | |
| **Alpha / mentored shift** | a newcomer's first, observed shift; mentors decide whether they join (manuals) | probation |
| **Cadre** | the group of leads who run one team by consensus, year-round (manuals) | |
| **Actual** | the board or LLC member on shift; faces outward (law enforcement, landowner, evictions); some burns use another word (manuals) | |
| **Handle** | the radio name a volunteer uses on duty, often their playa name; some departments approve and reserve them (manuals) | callsign (in the UI) |
| **Laminate (lam)** | the credential a volunteer or staff member wears (manuals) | badge |
| **HQ / outpost** | a department's base / a field post, each with a check-in point (manuals) | |
| **Walk-on** | a volunteer who turns up without a booked shift (manuals) | |
| **Must-report** | an incident a volunteer must call in even against the person's wishes (see Incidents) (manuals) | |
| **Perimeter** | a line of volunteers holding a crowd back from a burn (manuals) | |
| **Work weekend** | a scheduled pre-event work day on the land; an activity, not an event (manuals) | |
| **Gate / Greeters** | Gate checks tickets, wristbands and waivers; Greeters welcome arrivals and point them to camping (manuals) | |
| **Mutant vehicle / DMV** | an art car registered to drive on site / the department that registers and inspects them (manuals) | |
| **Exodus** | the departure period after the burn, often with a fixed off-the-land deadline (manuals) | |
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
- **At the event, the internet may be gone.** The work most often needed offline is incident
  management, volunteer management, and looking up a participant's personal and emergency
  information (owner, 2026-10-16).
- **Many people only have a phone** (owner, 2026-10-17). Every screen and flow must work
  on a small phone screen; being usable on a phone is a hard requirement, not a nicety.
- After the event, departments write after-burn reports and carry lessons into the next one.
  The department lead gathers volunteers' feedback into the report and inventories gear
  (manuals).
- **Per-event rules change from event to event** (manuals): gate hours (in and out), quiet
  hours and sound limits, burn-ban status, the exodus deadline. They are event data, not
  constants.
- **Sites are located by a map grid plus camp** (manuals): a lettered and numbered grid,
  camps numbered (often per zone), art with its own numbers. Volunteers give locations as
  grid and camp on the radio.

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

- **Departments contain standing teams** (manuals): for example, a Ranger department has
  specialist teams for peer crisis support, vehicles, mentoring, logistics and dispatch.
  Teams behave like positions with their own leads and sometimes their own shift times.
- **Chain of command is explicit and ranked** (manuals). A typical Ranger chain: field pair →
  shift supervisor → duty chief for the day → department lead → a safety-area facilitator
  coordinating all safety departments → the board member on shift. Each person reports to
  one person, and a supervisor has three to seven reports.
- **The safety family** (manuals): Rangers, Fire, Medical, Sanctuary (mental health),
  perimeter or event guardians, weather, sound, and a dispatch or logistics team that issues
  radios and credentials. A small burn folds several of these into Rangers.
- **Lead and specialist roles are selected, not self-signed** (manuals): a volunteer
  expresses interest, shadows a few shifts, and the leads decide.
- **Many people volunteer in several departments at once** (manuals), each with its own
  steps to complete.

## Volunteering and shifts

- **The volunteer lifecycle** (manuals): apply or express interest → required steps (a
  questionnaire, reading the manual, a quiz, a lead's approval) → training (often online
  first, then in person) → a first mentored shift → full member. Most teams require
  training **every year**, sometimes shorter for experienced returners.
- **Status follows recent activity** (manuals): for example active if they worked one of the
  last three events, inactive if one of the last five, otherwise retired. Status decides
  which training is needed and whether a returner must be re-evaluated on shift. One
  large department also reserves radio handles for long-serving volunteers.
- **Shift patterns** (manuals): field shifts of four to six hours (morning, afternoon, swing,
  graveyard); supervisors' shifts longer and overlapping the field shifts; duty chiefs 12
  hours, officers of the day 24. Volunteers arrive 15–20 minutes early for briefing,
  equipment and pairing, and stay for handover.
- **Some shifts are tied to a moment, not a clock time** (manuals): a burn perimeter starts
  about 90 minutes before ignition, and the time moves on the day.
- **Night and late-event shifts are hardest to fill** (manuals) and at some burns earn more
  credit.
- **Sign-up** (manuals): shifts have several slots; some teams confirm a request by hand
  before it counts; a volunteer may cancel (reopening the slot) and is asked to swap or tell
  the department early; some teams refuse walk-ons and late arrivals, others fill open
  slots at shift change. A shift can only be booked after the training it needs.
- **Check-in and check-out are recorded at a desk** (manuals). A missing check-out can
  trigger a search, and shift credit follows the check-out. Volunteers are on duty until
  released, and a lead may send a tired volunteer home without it counting against them.
- **Volunteers choose shifts partly by who else is on them** (owner, 2026-10-20). Some people
  don't get along, so being able to see who is on a shift before booking lets a volunteer pick
  another. Fellow volunteers see the playa name only, never legal names or contact details,
  and whether the roster is shown at all varies by regional (configurable).
- **Who sees which name** (owner, 2026-10-20, D43): at burns, playa names are generally public
  to anyone who volunteers, and volunteers' schedules (who is on which shift) are visible to
  other volunteers. Real (legal) names are usually visible only to the volunteer coordinator
  and perhaps a few other roles. Some burns want different visibility, so it is a regional
  setting, not code.
  Burn names are what everyone sees; legal names stay with the people whose role needs them
  (owner, 2026-10-22).
- **Burn names are unique among the volunteers for one event** (owner, 2026-10-22), so
  volunteers, leads and radio traffic can tell people apart. The check happens when someone
  signs up to volunteer for that event. Burn names in general (across events, or for people
  not volunteering) are not required to be unique.
- **Pairing happens on the day** (manuals): the shift supervisor pairs experienced with new,
  never partners or spouses.
- **What volunteering earns differs** (manuals): at some burns hours earn ticket credit,
  meals or a working camp; at others "you cannot get in by volunteering".
- **Volunteers sign agreements** (manuals): a code of conduct (sober on shift, no using the
  role for privilege, confidentiality), and a harassment policy acknowledgement.

## Qualifications and requirements

- Positions and shifts have **requirements** (owner, 2026-10-16): training completed,
  certificates, being over 18. Examples on a Ranger team: **Khaki** requires ICS-100 and
  training; **R12** requires ICS-200 and training.
- **ICS-100, ICS-200**: Incident Command System courses (FEMA, United States), common
  requirements for safety and emergency positions. Certificates can be recorded with the
  date earned; whether they expire depends on the regional.
- **Requirements combine several kinds** (manuals): training completed within the last N
  months (often 12), certificates (ICS-100, ICS-200, first aid, CPR, naloxone), experience
  counted in shifts (for example six for a shift supervisor, ten for a duty chief, varying by
  burn), a minimum age, an authorization (such as driving a department vehicle), and a lead's
  approval as an override.
- **Department leads check off** the qualifications that relate to their department
  (owner, 2026-10-16). Most qualifications belong to one department; a few, such as ICS-100,
  count for several departments (owner, 2026-10-16).
- **Qualifications are checked by hand** (owner, 2026-10-20). Nobody's training is verified
  by software: departments keep training records, and a lead looks up the person's last
  training and applies it to them. A recorded qualification has the date acquired, the date it
  expires (or none, indefinite) and who approved it.
- **Only department leads verify qualifications** (owner, 2026-10-20). Never ordinary
  volunteers, and never self-attested. A department-specific qualification is verified only by
  a lead of that department; an org-wide one can be verified by any department lead.
- **Leads may waive a missing requirement** (owner, 2026-10-20). A lead can assign someone
  who lacks a required qualification, as an explicit, recorded waiver (who waived, what, when).
  Volunteers can never waive a requirement for themselves; a lead may waive one for themself,
  as the same recorded waiver (owner, 2026-10-22).
- **Department-specific or org-wide.** Some qualifications belong to one department, such as
  a ranger department's field training or medical lead training; others are org-wide, such as
  ICS-100.
- **Two ways a qualification is recorded.** (1) A shift requires training: a lead looks up the
  person's last training date and records it for them. (2) A shift requires a certificate such
  as ICS-100: the person uploads a copy and a lead certifies the document is authentic; for an
  org-wide one any lead can do this.

## Incidents

- **Must-reports** (manuals), called in at once even against the person's wishes: absent or
  found child, child or elder abuse, domestic violence, sexual assault, non-consensual
  violence, medical emergency or death, psychiatric emergency, danger to a volunteer, grave
  danger to a participant, a drawn weapon. "Child" and "elder" ages vary by jurisdiction
  (for example under 18, 65 and over), so they are settings.
- **The shift supervisor owns the report** (manuals). Field volunteers call it in; only the
  supervisor escalates to law enforcement or medical. The supervisor also keeps a shift log
  of significant radio events with times.
- **What a report holds** (manuals): time, location (grid and camp), people involved (legal
  and playa names when known, descriptions), statements, severity, who responded, actions
  taken, promises left open, vehicle or tag numbers, law-enforcement officers and vehicles,
  and follow-up. Written as a factual timeline, "part of a legal evidence trail".
- **Identity is often partial** (manuals): peer-support teams record only a first or playa
  name, an approximate age, how the person arrived, the main concern, their camp and
  medication; they need to tell whether a returning person is the same one.
- **An absent child** is the one case where a real name goes on the radio; the gate usually
  closes until the child is found, law enforcement is called after a set time, and a child
  is released only against the guardian's details on the minor's waiver (manuals).
- **Handoffs are short and structured** (manuals): to medical, mental health, the Ranger
  supervisor, law enforcement or a sexual-assault advocate team. The receiver asks questions
  before the sender leaves.
- **Repeat offenders are tracked** (manuals): noisy camps, vehicle tags, street blockages,
  drones.
- **At the event, paper and radio come first** (manuals): reports are written on paper or
  entered at a kiosk, and typed in later. Software supplements radio; it doesn't replace it.

## Radios and equipment

- **Radio rules** (manuals): call the other party twice, then your handle; give locations as
  grid and camp with a phonetic alphabet; no real names, diagnoses or words like "dead",
  "drugs" or "eviction", because radio is overheard. Sensitive detail goes face to face.
- **Channels are assigned by function** (manuals): safety (repeater and simplex fallback),
  operations, incident command, talk, quiet channels assigned to individuals, and
  other-department channels.
- **Radios are checked out and back** (manuals): radio number, accessories, time out and in,
  condition, and who has it; often an ID card is held as collateral. Some people keep a radio
  for the whole event by role. Unreturned radios are chased.

## Conduct and complaints

- **Complaints about volunteers go to a neutral team** (manuals), not the accused's own
  department lead: a personnel or people-operations function investigates, and findings go
  only to those involved and the governing council. Retaliation against a reporter is itself
  a violation. Anonymous reports are allowed in spirit but "nearly impossible to
  investigate".
- **Discipline is a ladder** (manuals): sent home for a shift, removed from shifts, credential
  removed for the event, removed from the department (by the council, usually after the
  event).

## Registration before the event

- **Things participants register in advance** (manuals): theme camps (feeding placement;
  including RVs, sound and power), art (placement, power, sound; burnable art needs
  approval), sound systems (a sound-policy agreement), mutant vehicles (registration and
  inspection), service animals, drones, fire pits and fire performance, media.
- **At the gate** (manuals): ticket and ID, a signed waiver in the legal name, a wristband;
  minors need a guardian with legal authority and a waiver each.

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
- **Even the existence of a record is sensitive** (manuals): a peer-support team answers
  "is my friend with you?" with discretion. A person can sign a release letting the team
  tell named friends or campmates where they are.
- **Peer support isn't treatment** (manuals): those teams avoid clinical language, don't
  diagnose and keep notes minimal. Confidentiality gives way only when someone needs more
  help.
- **Volunteers must not repeat privileged information** (manuals), meaning anything they
  learned from internal lists or the department's systems.

## Between regionals

- Groups of regionals in one area may agree to recognize each other's volunteer credit, for
  example a regional Ranger council whose member burns count Ranger shifts at any of them
  (owner, 2026-10-16; D18, Q-TRUST).

## How regionals differ

When a rule above says "differs by regional", it becomes a setting or data in bms, never
code. Known differences so far: leadership terms, who may see incident reports, how much a
person sees about views of their data, whether legal name and phone are required, who sees playa names, legal names and the shift roster (owner, 2026-10-20),
open or invitation-only sign-up, terminology, and the default language: most use English,
some another (owner, 2026-10-16). From the manuals: position names (one burn's "shadows" are
another's "trackers"), qualification thresholds (experience shifts, training validity), the
age that counts as a child or elder, what volunteering earns, whether shifts need a lead's
approval, whether volunteers see who else is on a shift (owner, 2026-10-20), check-in windows,
and per-event rules (gate hours, quiet hours, burn bans).
