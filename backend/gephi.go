package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// buildGephiFile walks the datastore and returns a byte array containing a GML file
// describing the graph it found.
func buildGephiFile(rootHandle *RootHandle, fetchedHandles []*FetchedHandle) []byte {
	m := make(map[string]bool)
	m[rootHandle.Node.TwitterID] = true
	for _, friendID := range rootHandle.Node.FriendIDs {
		m[friendID] = true
	}
	for _, followerID := range rootHandle.Node.FollowerIDs {
		m[followerID] = true
	}
	w := new(bytes.Buffer)
	fmt.Fprintf(w, `graph [
  directed 1`)
	writeNode(w, &rootHandle.Node)
	for _, fetchedHandle := range fetchedHandles {
		writeNode(w, &fetchedHandle.Node)
	}
	e := make(map[string]bool)
	appendEdgeSet(e, m, &rootHandle.Node)
	for _, fetchedHandle := range fetchedHandles {
		appendEdgeSet(e, m, &fetchedHandle.Node)
	}
	writeEdges(w, e)
	fmt.Fprintf(w, "\n]")
	return w.Bytes()
}

// writeNode appends the node labels in the current GephiNode to the writer.
func writeNode(w io.Writer, n *GephiNode) {
	fmt.Fprintf(w, ` 
  node [ 
    id %v 
    user_id "%v" 
    label "%s" 
    type "%s" 
    profile_url "%s"
    description "%s"
    profile_image_url "%s"
    friends %v 
    followers %v 
  ]`,
		n.TwitterID, n.TwitterID, n.ScreenName, n.Relationship, n.ProfileURL, n.Description, n.ProfileImageURL, n.FriendsCount, n.FollowersCount)
}

// appendEdgeSet appends edges from the given GephiNode to the passed in set.
// The keys of the set will be "source target"
func appendEdgeSet(edgeSet map[string]bool, validIDs map[string]bool, n *GephiNode) {
	for _, follower := range n.FollowerIDs {
		if !validIDs[follower] {
			continue
		}
		edgeSet[fmt.Sprintf("%v %v", follower, n.TwitterID)] = true
	}
	for _, friend := range n.FriendIDs {
		if !validIDs[friend] {
			continue
		}
		edgeSet[fmt.Sprintf("%v %v", n.TwitterID, friend)] = true
	}
}

// writeEdges appends the edges from the given edge set to the writer.
func writeEdges(w io.Writer, edgeSet map[string]bool) {
	for edge, _ := range edgeSet {
		splits := strings.Split(edge, " ")
		fmt.Fprintf(w, ` 
  edge [ 
    source %v 
    target %v 
  ]`,
			splits[0], splits[1])
	}
}
