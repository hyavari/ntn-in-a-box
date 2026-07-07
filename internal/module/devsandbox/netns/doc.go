// Package netns creates and tears down Linux network namespaces with
// veth pairs and NAT routing for the Dev Sandbox module. It provides
// the isolated network environment that tc/netem shaping is applied to.
package netns
