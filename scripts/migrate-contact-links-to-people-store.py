#!/usr/bin/env python3
"""Copy multichat's contact links into people-store, once, on 2026-10-01.

For a few hours on 2026-10-01, before the move to people-store, multichat linked
bridge accounts to principal-store ids in its own tables
(contact_identity_links, contact_reviews, contact_link_refusals,
contact_link_suggestions). people-store owns "person" now. This script makes
one person per linked principal and attaches its accounts as matrix_user
identities, keeping linked_by, reason and linked_at; copies the reviews into
people-store's review queue; and refuses to run if there are refusals or open
suggestions, which it does not know how to carry.

A person gets principal_id only when the principal is a human (someone who
logs in); a contact principal's name is copied as the display name and its id
is not kept.

Idempotent: a principal whose accounts all belong to one person already is
skipped; one whose accounts are partly attached gets the rest added to that
person; accounts split across persons stop the run. Reviews are upserts.
people-store itself moves each attached handle out of any waiting suggestion
and supersedes a suggestion left empty; the dry run predicts how many.

  migrate-contact-links-to-people-store.py --multichat-db PATH \\
      --people-store-url URL --principal-store-url URL [--dry-run]
"""
import argparse
import collections
import json
import sqlite3
import sys
import urllib.error
import urllib.parse
import urllib.request

KNOWN_PLATFORMS = ["whatsapp", "telegram", "signal", "meta", "discord", "slack", "gmessages"]


def platform_of(user_id):
    localpart = user_id.split(":", 1)[0]
    for platform in KNOWN_PLATFORMS:
        if localpart.startswith("@" + platform + "_"):
            return platform
    raise SystemExit(f"{user_id} is not a bridge puppet id")


def call(method, url, body=None, missing_ok=False):
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(url, data=data, method=method)
    if data is not None:
        request.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            text = response.read()
            return json.loads(text) if text else None
    except urllib.error.HTTPError as error:
        if missing_ok and error.code == 404:
            return None
        raise SystemExit(f"{method} {url}: {error.code} {error.read().decode().strip()}")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--multichat-db", required=True)
    parser.add_argument("--people-store-url", required=True)
    parser.add_argument("--principal-store-url", required=True)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    people = args.people_store_url.rstrip("/")
    principals = args.principal_store_url.rstrip("/")

    database = sqlite3.connect(f"file:{args.multichat_db}?mode=ro", uri=True)
    links = database.execute(
        "SELECT contact_user_id, principal_id, linked_by, reason, linked_at FROM contact_identity_links ORDER BY linked_at, contact_user_id").fetchall()
    reviews = database.execute("SELECT contact_user_id, reviewed_by, reviewed_at FROM contact_reviews").fetchall()
    refusals = database.execute("SELECT COUNT(*) FROM contact_link_refusals").fetchone()[0]
    open_suggestions = database.execute("SELECT COUNT(*) FROM contact_link_suggestions WHERE status = 'open'").fetchone()[0]
    if refusals or open_suggestions:
        raise SystemExit(f"{refusals} refusals and {open_suggestions} open suggestions in multichat; this script carries neither. Stop and decide how.")

    by_principal = collections.OrderedDict()
    for user_id, principal_id, linked_by, reason, linked_at in links:
        by_principal.setdefault(principal_id, []).append(
            {"kind": "matrix_user", "value": user_id, "label": platform_of(user_id),
             "linked_by": linked_by, "reason": reason, "linked_at": linked_at})

    # The waiting suggestions and their handles, to predict which ones the
    # migration supersedes.
    waiting = {}
    offset = 0
    while True:
        page = call("GET", f"{people}/suggestions?status=proposed&limit=1000&offset={offset}")
        for suggestion in page:
            waiting[suggestion["id"]] = {identity["value"] for identity in suggestion["payload"]["identities"] if identity["kind"] == "matrix_user"}
        if len(page) < 1000:
            break
        offset += 1000

    plan = {"principals": len(by_principal), "links": len(links), "persons_to_create": 0, "persons_already_migrated": 0,
            "persons_to_extend": 0, "identities_to_add": 0, "identities_already_attached": 0,
            "persons_with_principal_id": 0, "reviews_to_copy": len(reviews)}
    created = []
    moving = set()
    for principal_id, identities in by_principal.items():
        principal = call("GET", f"{principals}/principals/{urllib.parse.quote(principal_id)}")
        owners = {}
        for identity in identities:
            resolved = call("GET", f"{people}/identities/resolve?kind=matrix_user&value={urllib.parse.quote(identity['value'])}", missing_ok=True)
            if resolved:
                owners[identity["value"]] = resolved["person"]["id"]
        owner_ids = set(owners.values())
        if len(owner_ids) > 1:
            raise SystemExit(f"{principal_id}'s accounts already belong to several persons {sorted(owner_ids)}; resolve by hand")
        missing = [identity for identity in identities if identity["value"] not in owners]
        plan["identities_already_attached"] += len(identities) - len(missing)
        moving.update(identity["value"] for identity in missing)
        if not missing:
            plan["persons_already_migrated"] += 1
            continue
        plan["identities_to_add"] += len(missing)
        if owner_ids:
            person_id = owner_ids.pop()
            plan["persons_to_extend"] += 1
            if not args.dry_run:
                for identity in missing:
                    call("POST", f"{people}/persons/{person_id}/identities", identity)
            created.append(f"{person_id} {principal['display_name']} +{len(missing)} (extended)")
            continue
        body = {"display_name": principal["display_name"], "identities": missing}
        if principal["kind"] == "human":
            body["principal_id"] = principal_id
            plan["persons_with_principal_id"] += 1
        plan["persons_to_create"] += 1
        if args.dry_run:
            created.append(f"(new) {principal['display_name']} from {principal_id} ({principal['kind']}), {len(missing)} handles")
        else:
            person = call("POST", f"{people}/persons", body)
            created.append(f"{person['id']} {person['display_name']} from {principal_id} ({principal['kind']}), {len(person['identities'])} handles")

    superseded = sum(1 for handles in waiting.values() if handles and handles <= moving)
    shrunk = sum(1 for handles in waiting.values() if handles & moving and not handles <= moving)
    plan["suggestions_to_supersede"] = superseded
    plan["suggestions_to_shrink"] = shrunk

    by_batch = collections.defaultdict(list)
    for user_id, reviewed_by, reviewed_at in reviews:
        by_batch[(reviewed_by, reviewed_at)].append({"kind": "matrix_user", "value": user_id})
    if not args.dry_run:
        for (reviewed_by, reviewed_at), handles in by_batch.items():
            call("POST", f"{people}/reviews", {"handles": handles, "reviewed_by": reviewed_by, "reviewed_at": reviewed_at})

    for line in created:
        print(line)
    plan["dry_run"] = args.dry_run
    print(json.dumps(plan))


if __name__ == "__main__":
    sys.exit(main())
