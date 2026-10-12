package ui

import (
	"fmt"

	"github.com/joangrigorov/ghspector/internal/store"
)

// The GitHub pages the conversation screens point at.
//
// Every builder returns an empty string when it does not have what the URL
// needs, because hyperlink treats "" as "draw the text and nothing else": a
// caller never has to ask whether the link is real.
//
// A URL is built from what the API returned rather than from the screen's own
// state, so a link cannot point at a repository the row it is drawn from does
// not belong to.

// repoPageURL is a repository's page, which the API's full name already spells
// out as the owner and the name.
func repoPageURL(fullName string) string {
	if fullName == "" {
		return ""
	}
	return "https://github.com/" + fullName
}

// compareURL is the diff between a base branch and a head branch, which is the
// page a reviewer wants from "Source: x → Base: y". It answers for nothing when
// either branch is missing, because a compare needs both.
func compareURL(fullName, base, head string) string {
	if fullName == "" || base == "" || head == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/compare/%s...%s", fullName, base, head)
}

// threadURL is where a thread can be read on GitHub: the permalink of the remark
// it started from.
//
// The permalink is the answer rather than a link to the file at the line the
// comment names, because GitHub keeps a comment on the line it was written
// against: the API reports that line as zero once the diff has moved past it,
// while the permalink still lands on the remark.
func threadURL(thread reviewThread) string {
	for _, comment := range thread.comments {
		if comment.HTMLURL != "" {
			return comment.HTMLURL
		}
	}
	return ""
}

// repoFullName is the repository a screen is showing: the name the payload
// carried when it carried one, and the reference the route was opened with
// otherwise.
//
// The REST detail payloads for a pull request and for an issue name the
// repository only under head and base, so the top-level field is empty on a real
// screen — the live check for this change found the heading drawing the
// reference while linking nothing, because the links were built from that empty
// field. Both the text and its links come through here.
func repoFullName(payload string, ref store.RepoRef) string {
	if payload != "" {
		return payload
	}
	return ref.String()
}

// issueCommentsURL is an issue's page, for the conversation screen of an issue
// the list has not handed over: the route knows the repository and the number.
func issueCommentsURL(ref store.RepoRef, number int) string {
	if !ref.Valid() || number <= 0 {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", ref.String(), number)
}
