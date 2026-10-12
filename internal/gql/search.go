package gql

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/shurcooL/githubv4"

	"github.com/joangrigorov/ghspector/internal/gh"
)

// SearchKind selects the index a search reads.
type SearchKind string

const (
	// KindIssue searches issues, with pull requests excluded by the query.
	KindIssue SearchKind = "issue"
	// KindPullRequest searches pull requests.
	KindPullRequest SearchKind = "pr"
)

// SearchInput describes one page of a scope-wide search.
type SearchInput struct {
	Kind SearchKind
	// Owner is the user or organization login the scope covers, and IsOrg says
	// which of the two it is.
	Owner string
	IsOrg bool
	// Repo, Author, Assignee and Reviewer narrow the search. An empty field
	// narrows nothing. Reviewer applies to pull requests only.
	Repo     string
	Author   string
	Assignee string
	Reviewer string
	// State is open, closed, or empty for both.
	State string
	// First is the page size, After the cursor that starts this page.
	First int
	After string
}

// SearchPage is one page of results with the totals the API reported for it.
type SearchPage[T any] struct {
	Items []T
	// Total is how many matches the index reports for the whole query. The API
	// caps it at 1000 regardless of the true number.
	Total int
	// NextCursor starts the page after this one, and is empty when HasNext is
	// false.
	NextCursor string
	// HasNext reports whether another page exists.
	HasNext bool
	// Budget is what the query cost.
	Budget Budget
}

// SearchIssues returns one page of the issues matching in.
func (c *Client) SearchIssues(ctx context.Context, in SearchInput) (SearchPage[gh.Issue], error) {
	in.Kind = KindIssue
	var q issuesQuery
	// The budget is read through a closure rather than a bound method value: a
	// method value copies the receiver, and the copy would be taken before the
	// response is decoded into it.
	if err := c.run(ctx, &q, in, func() Budget { return q.RateLimit.budget() }); err != nil {
		return SearchPage[gh.Issue]{}, err
	}
	page := pageOf[gh.Issue](q.Search.IssueCount, q.Search.PageInfo, q.RateLimit.budget())
	for _, node := range q.Search.Nodes {
		page.Items = append(page.Items, node.Issue.issue())
	}
	return page, nil
}

// SearchPullRequests returns one page of the pull requests matching in.
func (c *Client) SearchPullRequests(ctx context.Context, in SearchInput) (SearchPage[gh.PullRequest], error) {
	in.Kind = KindPullRequest
	var q pullRequestsQuery
	if err := c.run(ctx, &q, in, func() Budget { return q.RateLimit.budget() }); err != nil {
		return SearchPage[gh.PullRequest]{}, err
	}
	page := pageOf[gh.PullRequest](q.Search.IssueCount, q.Search.PageInfo, q.RateLimit.budget())
	for _, node := range q.Search.Nodes {
		page.Items = append(page.Items, node.PullRequest.pullRequest())
	}
	return page, nil
}

// run executes one search.
func (c *Client) run(ctx context.Context, q any, in SearchInput, read func() Budget) error {
	after := (*githubv4.String)(nil)
	if in.After != "" {
		after = githubv4.NewString(githubv4.String(in.After))
	}
	first := in.First
	if first < 1 {
		first = 1
	}
	variables := map[string]any{
		"q":     githubv4.String(qualifiers(in)),
		"first": githubv4.Int(first),
		"after": after,
	}
	return c.do(ctx, q, variables, read)
}

// pageOf assembles the parts of a page that do not depend on the result type.
func pageOf[T any](total int, info pageInfo, budget Budget) SearchPage[T] {
	return SearchPage[T]{
		Total:      total,
		NextCursor: info.EndCursor,
		HasNext:    info.HasNextPage,
		Budget:     budget,
	}
}

// qualifiers renders the search query string for in.
func qualifiers(in SearchInput) string {
	parts := []string{"sort:updated-desc"}
	if in.IsOrg {
		parts = append(parts, "org:"+quoteQualifier(in.Owner))
	} else {
		parts = append(parts, "user:"+quoteQualifier(in.Owner))
	}
	if in.Kind == KindPullRequest {
		parts = append(parts, "is:pr")
	} else {
		parts = append(parts, "is:issue")
	}
	if in.Repo != "" {
		parts = append(parts, "repo:"+quoteQualifier(in.Owner+"/"+in.Repo))
	}
	// An absent state qualifier matches both open and closed, which is what the
	// "all" step of the state cycle means.
	if in.State != "" && in.State != "all" {
		parts = append(parts, "state:"+in.State)
	}
	if in.Author != "" {
		parts = append(parts, "author:"+quoteQualifier(appQualifier(in.Author)))
	}
	if in.Assignee != "" {
		parts = append(parts, "assignee:"+quoteQualifier(appQualifier(in.Assignee)))
	}
	if in.Reviewer != "" && in.Kind == KindPullRequest {
		parts = append(parts, "review-requested:"+quoteQualifier(appQualifier(in.Reviewer)))
	}
	return strings.Join(parts, " ")
}

// appQualifier renders a login the way the search index reads an app.
//
// The index knows an app as "app/<slug>" and matches neither spelling the API
// uses for the same account: the bare slug a bot actor reports ("github-actions")
// returns nothing, and the "[bot]" suffix the REST transport puts on the login
// is dropped by quoteQualifier and would match nothing either. Verified against
// the live index for author: — user:joangrigorov author:app/github-actions
// returns the app's pull request, both other spellings return none. A login that
// is already "app/<slug>" is passed through, and anything else, a person's
// login included, is left alone.
func appQualifier(login string) string {
	if strings.HasPrefix(login, "app/") {
		return login
	}
	if slug, ok := strings.CutSuffix(login, "[bot]"); ok {
		return "app/" + slug
	}
	return login
}

// quoteQualifier wraps a qualifier value in quotes when it holds anything a
// search query would otherwise read as syntax, so a stray space in a filter
// value cannot add a qualifier of its own. Characters that can appear in neither
// a login nor a repository name are dropped instead of escaping into the query.
func quoteQualifier(v string) string {
	var b strings.Builder
	quoted := false
	for _, r := range v {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.' || r == '/':
			b.WriteRune(r)
		case r == ' ':
			quoted = true
			b.WriteRune(r)
		}
	}
	if !quoted {
		return b.String()
	}
	return `"` + b.String() + `"`
}

// budgetField is the rateLimit block every query asks for, so each response
// reports what it cost and what is left. The field name reaches the API as
// "rateLimit".
type budgetField struct {
	Limit     githubv4.Int
	Cost      githubv4.Int
	Remaining githubv4.Int
	ResetAt   time.Time
}

func (b budgetField) budget() Budget {
	return Budget{
		Limit:     int(b.Limit),
		Cost:      int(b.Cost),
		Remaining: int(b.Remaining),
		ResetAt:   b.ResetAt,
	}
}

// pageInfo is the cursor half of a connection.
type pageInfo struct {
	EndCursor   string
	HasNextPage bool
}

// actorFields is an actor's login and the id of the concrete type behind it.
//
// The Actor interface carries the login and nothing else — databaseId lives on
// User, Bot and the other implementors — so each arm that has one is selected
// through its own fragment. Requesting databaseId on the interface is rejected
// by the API with "Field 'databaseId' doesn't exist on type 'Actor'". An actor
// whose type carries no id, such as a mannequin, still yields its login.
type actorFields struct {
	Login string
	User  struct {
		DatabaseID int
	} `graphql:"... on User"`
	// __typename is how a bot is recognised: the login alone cannot say whether
	// "github-actions" is an app or a person. Only the arm a response matches
	// carries it, so it is set for a bot and empty for anything else.
	Bot struct {
		TypeName   string `graphql:"__typename"`
		DatabaseID int
	} `graphql:"... on Bot"`
}

// databaseID is the actor's id, or zero for a type that carries none.
func (a actorFields) databaseID() int {
	if a.User.DatabaseID != 0 {
		return a.User.DatabaseID
	}
	return a.Bot.DatabaseID
}

// login is the actor's login as the REST transport reports it. An app's account
// is "github-actions" here and "github-actions[bot]" there, and the difference
// reaches the screen and the filter built from it, so the suffix is put back.
func (a actorFields) login() string {
	if a.Bot.TypeName != "" && !strings.HasSuffix(a.Login, "[bot]") {
		return a.Login + "[bot]"
	}
	return a.Login
}

func (a actorFields) user() *gh.User {
	if a.Login == "" {
		return nil
	}
	return &gh.User{Login: a.login(), ID: int64(a.databaseID())}
}

// userFields is a login with the id of a User, for the positions whose type is
// the concrete User — the assignee connection and the review request union,
// both of which are typed User. An inline fragment is unnecessary there, and a
// Bot fragment could not be spread inside a User at all.
type userFields struct {
	Login      string
	DatabaseID int
}

func (u userFields) user() *gh.User {
	if u.Login == "" {
		return nil
	}
	return &gh.User{Login: u.Login, ID: int64(u.DatabaseID)}
}

type labelFields struct {
	Name        string
	Color       string
	Description string
}

// labelConnection is a connection of one node type.
type labelConnection struct {
	Nodes []labelFields
}

func (l labelConnection) labels() []gh.Label {
	if len(l.Nodes) == 0 {
		return nil
	}
	out := make([]gh.Label, 0, len(l.Nodes))
	for _, n := range l.Nodes {
		out = append(out, gh.Label{Name: n.Name, Color: n.Color, Description: n.Description})
	}
	return out
}

// actorConnection is a connection of users, as the assignees of an issue or a
// pull request are.
type actorConnection struct {
	Nodes []userFields
}

func (a actorConnection) users() []gh.User {
	if len(a.Nodes) == 0 {
		return nil
	}
	out := make([]gh.User, 0, len(a.Nodes))
	for _, n := range a.Nodes {
		if u := n.user(); u != nil {
			out = append(out, *u)
		}
	}
	return out
}

type milestoneFields struct {
	Title string
}

// repositoryFields is the repository a result belongs to.
type repositoryFields struct {
	DatabaseID       int
	Name             string
	NameWithOwner    string
	PushedAt         time.Time
	DefaultBranchRef *struct {
		Name string
	}
	Owner struct {
		User struct {
			Login      string
			DatabaseID int
		} `graphql:"... on User"`
		Organization struct {
			Login      string
			DatabaseID int
		} `graphql:"... on Organization"`
	}
}

func (r repositoryFields) repository() gh.Repository {
	owner := r.Owner.User
	if owner.Login == "" {
		owner = r.Owner.Organization
	}
	repo := gh.Repository{
		ID:       int64(r.DatabaseID),
		Name:     r.Name,
		FullName: r.NameWithOwner,
		PushedAt: r.PushedAt,
	}
	if owner.Login != "" {
		repo.Owner = &gh.User{Login: owner.Login, ID: int64(owner.DatabaseID)}
	}
	if r.DefaultBranchRef != nil {
		repo.DefaultBranch = r.DefaultBranchRef.Name
	}
	return repo
}

// ownerLogin reports the account the repository belongs to.
func (r repositoryFields) ownerLogin() string {
	if r.Owner.User.Login != "" {
		return r.Owner.User.Login
	}
	return r.Owner.Organization.Login
}

// issueFields is the Issue fragment of a search result.
type issueFields struct {
	DatabaseID int
	Number     int
	Title      string
	Body       string
	State      string
	URL        string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ClosedAt   *time.Time
	Author     *actorFields
	Assignees  actorConnection `graphql:"assignees(first: 10)"`
	Labels     labelConnection `graphql:"labels(first: 20)"`
	Milestone  *milestoneFields
	Repository repositoryFields
}

func (f issueFields) issue() gh.Issue {
	issue := gh.Issue{
		ID:         int64(f.DatabaseID),
		Number:     f.Number,
		Title:      f.Title,
		Body:       f.Body,
		State:      strings.ToLower(f.State),
		HTMLURL:    f.URL,
		CreatedAt:  f.CreatedAt,
		UpdatedAt:  f.UpdatedAt,
		ClosedAt:   f.ClosedAt,
		User:       f.Author.user(),
		Assignees:  f.Assignees.users(),
		Labels:     f.Labels.labels(),
		Repository: f.Repository.repository(),
	}
	if f.Milestone != nil {
		issue.Milestone = &gh.Milestone{Title: f.Milestone.Title}
	}
	return issue
}

// pullRequestsQuery is the scope-wide pull request search. Search results are a
// union, so each node carries an inline fragment for the member it is.
type pullRequestsQuery struct {
	RateLimit budgetField
	Search    struct {
		IssueCount int
		Nodes      []struct {
			PullRequest pullRequestFields `graphql:"... on PullRequest"`
		}
		PageInfo pageInfo
	} `graphql:"search(query: $q, type: ISSUE, first: $first, after: $after)"`
}

// issuesQuery is the scope-wide issue search. Pull requests are excluded by the
// query's is:issue qualifier rather than by a filter over the page.
type issuesQuery struct {
	RateLimit budgetField
	Search    struct {
		IssueCount int
		Nodes      []struct {
			Issue issueFields `graphql:"... on Issue"`
		}
		PageInfo pageInfo
	} `graphql:"search(query: $q, type: ISSUE, first: $first, after: $after)"`
}

// pullRequestFields is the PullRequest fragment of a search result.
type pullRequestFields struct {
	DatabaseID     int
	Number         int
	Title          string
	Body           string
	State          string
	IsDraft        bool
	URL            string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	MergedAt       *time.Time
	Author         *actorFields
	Assignees      actorConnection `graphql:"assignees(first: 10)"`
	Labels         labelConnection `graphql:"labels(first: 20)"`
	Milestone      *milestoneFields
	BaseRefName    string
	HeadRefName    string
	HeadRefOid     string
	HeadRepository *repositoryFields
	Repository     repositoryFields
	// Only the User arm of the RequestedReviewer union is selected: the domain
	// type holds logins, and the REST list leaves team reviewers empty too.
	ReviewRequests struct {
		Nodes []reviewRequestNode
	} `graphql:"reviewRequests(first: 10)"`
}

// reviewRequestNode is one review request of a pull request. The union carries
// no fields of its own, so the login comes from the fragment, and the field that
// names the JSON key (requestedReviewer) carries no tag: a field tagged with a
// fragment writes the fragment in place of its name and becomes invisible to the
// response decoder.
type reviewRequestNode struct {
	RequestedReviewer struct {
		User userFields `graphql:"... on User"`
	}
}

func (f pullRequestFields) pullRequest() gh.PullRequest {
	pr := gh.PullRequest{
		ID:                 int64(f.DatabaseID),
		Number:             f.Number,
		Title:              f.Title,
		Body:               f.Body,
		State:              pullRequestState(f.State),
		Draft:              f.IsDraft,
		HTMLURL:            f.URL,
		CreatedAt:          f.CreatedAt,
		UpdatedAt:          f.UpdatedAt,
		MergedAt:           f.MergedAt,
		User:               f.Author.user(),
		Assignees:          f.Assignees.users(),
		Labels:             f.Labels.labels(),
		Repository:         f.Repository.repository(),
		Base:               gh.PullRequestRef{Ref: f.BaseRefName, Label: refLabel(f.Repository.ownerLogin(), f.BaseRefName)},
		Head:               gh.PullRequestRef{Ref: f.HeadRefName, SHA: f.HeadRefOid, Label: refLabel(f.headOwner(), f.HeadRefName)},
		RequestedReviewers: requestedReviewers(f.ReviewRequests.Nodes),
	}
	if f.Milestone != nil {
		pr.Milestone = &gh.Milestone{Title: f.Milestone.Title}
	}
	if f.HeadRepository != nil {
		head := f.HeadRepository.repository()
		pr.Head.Repo = &head
	}
	return pr
}

// headOwner reports the account a pull request was opened from, falling back to
// the base repository when the head repository is not visible.
func (f pullRequestFields) headOwner() string {
	if f.HeadRepository != nil {
		if login := f.HeadRepository.ownerLogin(); login != "" {
			return login
		}
	}
	return f.Repository.ownerLogin()
}

// requestedReviewers flattens the review request connections to the logins they
// hold.
func requestedReviewers(nodes []reviewRequestNode) []gh.User {
	var out []gh.User
	for _, n := range nodes {
		if u := n.RequestedReviewer.User.user(); u != nil {
			out = append(out, *u)
		}
	}
	return out
}

// refLabel renders a ref the way the REST transport reports it: "owner:branch".
func refLabel(owner, ref string) string {
	if owner == "" || ref == "" {
		return ref
	}
	return owner + ":" + ref
}

// pullRequestState maps the GraphQL state enum onto the string the REST
// transport reports. A merged pull request is closed, with the merge time
// carried separately.
func pullRequestState(state string) string {
	switch strings.ToUpper(state) {
	case "OPEN":
		return "open"
	case "CLOSED", "MERGED":
		return "closed"
	}
	return strings.ToLower(state)
}
