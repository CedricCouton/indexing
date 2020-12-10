package kvutility

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"

	common "github.com/couchbase/indexing/secondary/common"
	"github.com/couchbase/indexing/secondary/common/collections"
	tc "github.com/couchbase/indexing/secondary/tests/framework/common"
)

func SetKeyValuesForCollection(keyValues tc.KeyValues, bucketName, collectionID, password, hostaddress string) {
	url := "http://" + bucketName + ":" + password + "@" + hostaddress

	b, err := common.ConnectBucket(url, "default", bucketName)
	tc.HandleError(err, "bucket")
	defer b.Close()

	for key, value := range keyValues {
		// The vb mapping for a key is independent of collections.
		// Pass key and collectionID separately for correct vb:key mapping
		err = b.SetC(key, collectionID, 0, value)
		tc.HandleError(err, "set")
	}
}

func GetFromCollection(key string, rv interface{}, bucketName, collectionID, password, hostaddress string) {
	url := "http://" + bucketName + ":" + password + "@" + hostaddress

	b, err := common.ConnectBucket(url, "default", bucketName)
	tc.HandleError(err, "bucket")
	defer b.Close()

	err = b.GetC(key, collectionID, &rv)
	tc.HandleError(err, "get")
}

func DeleteFromCollection(key string, bucketName, collectionID, password, hostaddress string) {
	url := "http://" + bucketName + ":" + password + "@" + hostaddress

	b, err := common.ConnectBucket(url, "default", bucketName)
	tc.HandleError(err, "bucket")
	defer b.Close()

	err = b.DeleteC(key, collectionID)
	tc.HandleError(err, "delete")
}

func DeleteKeysFromCollection(keyValues tc.KeyValues, bucketName, collectionID, password, hostaddress string) {
	url := "http://" + bucketName + ":" + password + "@" + hostaddress

	b, err := common.ConnectBucket(url, "default", bucketName)
	tc.HandleError(err, "bucket")
	defer b.Close()

	for key, _ := range keyValues {
		err = b.DeleteC(key, collectionID)
		tc.HandleError(err, "delete")
	}
}

func GetManifest(bucketName string, serverUserName, serverPassword, hostaddress string) *collections.CollectionManifest {
	client := &http.Client{}
	address := "http://" + hostaddress + "/pools/default/buckets/" + bucketName + "/collections"

	req, _ := http.NewRequest("GET", address, nil)
	req.SetBasicAuth(serverUserName, serverPassword)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	resp, err := client.Do(req)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		log.Printf(address)
		log.Printf("%v", req)
		log.Printf("%v", resp)
		log.Printf("GetCollectionsManifest failed for bucket %v \n", bucketName)
	}
	// todo : error out if response is error
	tc.HandleError(err, "GetCollectionsManifest")
	defer resp.Body.Close()

	manifest := &collections.CollectionManifest{}
	body, _ := ioutil.ReadAll(resp.Body)
	err = json.Unmarshal(body, manifest)
	if err != nil {
		tc.HandleError(err, fmt.Sprintf("GetCollectionsManifest :: Unmarshal of response body: %q", body))
	}
	return manifest
}

func GetScopes(bucketName, serverUserName, serverPassword, hostaddress string) []collections.CollectionScope {
	manifest := GetManifest(bucketName, serverUserName, serverPassword, hostaddress)
	return manifest.Scopes
}

func createScope(bucketName, scopeName, serverUserName, serverPassword, hostaddress string) {
	client := &http.Client{}
	address := "http://" + hostaddress + "/pools/default/buckets/" + bucketName + "/collections/"
	data := url.Values{"name": {scopeName}}
	req, _ := http.NewRequest("POST", address, strings.NewReader(data.Encode()))
	req.SetBasicAuth(serverUserName, serverPassword)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	resp, err := client.Do(req)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		log.Printf(address)
		log.Printf("%v", req)
		log.Printf("%v", resp)
		log.Printf("Create scope failed for bucket %v, scopeName: %v \n", bucketName, scopeName)

	}
	// todo : error out if response is error
	tc.HandleError(err, "Create scope "+address)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	log.Printf("Create scope succeeded for bucket %v, scopeName: %v, body: %s \n", bucketName, scopeName, body)

}

func createCollection(bucketName, scopeName, collectionName, serverUserName, serverPassword, hostaddress string) {
	client := &http.Client{}
	address := "http://" + hostaddress + "/pools/default/buckets/" + bucketName + "/collections/" + scopeName + "/"
	data := url.Values{"name": {collectionName}}
	req, _ := http.NewRequest("POST", address, strings.NewReader(data.Encode()))
	req.SetBasicAuth(serverUserName, serverPassword)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	resp, err := client.Do(req)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		log.Printf(address)
		log.Printf("%v", req)
		log.Printf("%v", resp)
		log.Printf("Create colleciton failed for bucket %v, scopeName: %v, collection: %v \n",
			bucketName, scopeName, collectionName)
	}
	// todo : error out if response is error
	tc.HandleError(err, "Create Collection "+address)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)

	log.Printf("Created collection succeeded for bucket: %v, scope: %v, collection: %v, body: %s", bucketName, scopeName, collectionName, body)

}

func CreateCollection(bucketName, scope, collection, serverUsername, serverPassword, hostaddress string) {
	// 1. get scopes for the bucket
	scopes := GetScopes(bucketName, serverUsername, serverPassword, hostaddress)
	present := false
	for _, s := range scopes {
		if scope == s.Name {
			present = true
			break
		}
	}
	// 2. Scope does not exist. Create the scope
	if !present {
		log.Printf("Creating scope: %v for bucket: %v as it does not exist", scope, bucketName)
		createScope(bucketName, scope, serverUsername, serverPassword, hostaddress)
	}
	createCollection(bucketName, scope, collection, serverUsername, serverPassword, hostaddress)
}

func DropScope(bucketName, scopeName, serverUserName, serverPassword, hostaddress string) {
	client := &http.Client{}
	address := "http://" + hostaddress + "/pools/default/buckets/" + bucketName + "/collections/" + scopeName
	req, _ := http.NewRequest("DELETE", address, nil)
	req.SetBasicAuth(serverUserName, serverPassword)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	resp, err := client.Do(req)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		log.Printf(address)
		log.Printf("%v", req)
		log.Printf("%v", resp)
		log.Printf("Drop scope failed for bucket %v, scope: %v \n", bucketName, scopeName)
	}
	// todo : error out if response is error
	tc.HandleError(err, "Drop scope "+address)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	log.Printf("Dropped scope %v for bucket: %v, body: %s", scopeName, bucketName, body)
}

func DropCollection(bucketName, scopeName, collectionName, serverUserName, serverPassword, hostaddress string) {
	client := &http.Client{}
	address := "http://" + hostaddress + "/pools/default/buckets/" + bucketName + "/collections/" + scopeName + "/" + collectionName
	req, _ := http.NewRequest("DELETE", address, nil)
	req.SetBasicAuth(serverUserName, serverPassword)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	resp, err := client.Do(req)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		log.Printf(address)
		log.Printf("%v", req)
		log.Printf("%v", resp)
		log.Printf("Drop collection failed for bucket %v, scope: %v, collection: %v \n", bucketName, scopeName)
	}
	// todo : error out if response is error
	tc.HandleError(err, "Drop scope "+address)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	log.Printf("Dropped collection %v for bucket: %v, scope: %v, body: %s", collectionName, bucketName, scopeName, body)
}

func GetCollectionID(bucketName, scopeName, collectionName, serverUserName, serverPassword, hostaddress string) string {
	manifest := GetManifest(bucketName, serverUserName, serverPassword, hostaddress)
	for _, scope := range manifest.Scopes {
		if scope.Name == scopeName {
			for _, collection := range scope.Collections {
				if collection.Name == collectionName {
					return collection.UID
				}
			}
		}
	}
	return ""
}

func GetScopeID(bucketName, scopeName, serverUserName, serverPassword, hostaddress string) string {
	manifest := GetManifest(bucketName, serverUserName, serverPassword, hostaddress)
	for _, scope := range manifest.Scopes {
		if scope.Name == scopeName {
			return scope.UID
		}
	}
	return ""
}

func DropAllScopesAndCollections(bucketName, serverUserName, serverPassword, hostaddress string, dropDefaultCollection bool) {

	manifest := GetManifest(bucketName, serverUserName, serverPassword, hostaddress)
	for _, scope := range manifest.Scopes {

		if scope.Name != common.DEFAULT_SCOPE {
			DropScope(bucketName, scope.Name, serverUserName, serverPassword, hostaddress)
		} else {
			for _, collection := range scope.Collections {
				if collection.Name != common.DEFAULT_COLLECTION {
					DropCollection(bucketName, scope.Name, collection.Name, serverUserName, serverPassword, hostaddress)
				} else {
					if dropDefaultCollection {
						DropCollection(bucketName, scope.Name, collection.Name, serverUserName, serverPassword, hostaddress)
					}
				}
			}
		}
	}
}
