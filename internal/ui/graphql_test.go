package ui

// The fixture server answers the REST endpoints. It does not model the GraphQL
// search index, and it says so on the path the app would query, so every UI test
// drives the documented fallback: the scope-wide lists fall back to the REST
// fan-out when a search is refused.
//
// The search path itself is covered where it lives. internal/gql asserts the
// query the app sends and the domain objects it maps out of a response, and the
// store's own tests assert that both transports return equivalent pull requests
// and issues for the same fixture, that paging follows cursors, and that a
// failed search falls back. What a UI test would add on top of that is a second
// rendering of data the screen has already been shown to render, so the fixture
// stays small and the coverage boundary is stated rather than implied.

// graphQLPath is where the fixture server refuses GraphQL queries.
const graphQLPath = "/graphql"
