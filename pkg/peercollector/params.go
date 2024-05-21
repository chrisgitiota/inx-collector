package peercollector

type Parameters struct {
	// HostUrl defines the address of the peer inx-collector instance used to synchronize storage contents
	HostUrl string `default:"" usage:"address of the peer inx-collector instance used to synchronize storage contents. 'http' or 'https' scheme needs to be included. "`
}
