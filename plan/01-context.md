# Context

> Draft from the kickoff conversation (2026-10-16). The owner should correct anything that
> doesn't match how regionals actually work.

## Why this exists

Regional burns run on volunteer labor, and a lot of that labor is coordination: who's
working which shift, which camps are coming and how big they are, where the art goes, what
went wrong and how it was handled, and what to change next year. Today that coordination
often lives in proprietary systems maintained by a single person, or in spreadsheets that
nobody can find a year later. When the one maintainer burns out or moves on, the regional
loses its tools and its history.

bms is meant to be the opposite: open source, built by many contributors, owned by the
community, and easy for any regional to run and adapt. It follows the principle of
**communal effort** in how it's built, not only in what it supports.

## Who uses it

| Role | Needs |
|---|---|
| Participant | Register, sign up for shifts, register a camp, vehicle or art, see their own data |
| Volunteer lead / department lead | Build shifts, see coverage, contact their team, check people in, report |
| Registration leads (placement, art, vehicles, events) | Review and approve applications, ask follow-up questions |
| Incident roles (rangers, medical, safety, leadership) | Log incidents quickly, triage, follow up, with strict privacy |
| Event organizers / board | Cross-department view, after-burn reports, year-over-year history |
| Site operator | Install, configure, back up and upgrade the software |

## Shape of the problem

- **Events, with work all year.** Most regionals hold one burn a year, some two. Data is
  organized by **department** and, where it applies, **event**, and history matters across
  events. Work happens well before and after the gates open (land prep, build, strike,
  inventory).
- **Departments differ by regional.** Names, structure and policies are data, not code.
- **Connectivity at the event can be poor or absent.** Some workflows (incident logging,
  shift check-in, gate) may need to work offline. See Q-OFFLINE.
- **Volunteer operators.** Whoever runs the server is probably a volunteer. Install, upgrade
  and backup must be simple and well documented.
- **Sensitive data.** Incident reports, medical notes and legal names carry real legal and
  personal risk if mishandled.
