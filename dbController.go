package main

import (
    "context"
    "time"
    "log"
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
    "encoding/json"
    "github.com/jeremywohl/flatten"
)

type DocumentStatus int64

const (
    OK DocumentStatus = iota
    NotFound
    CouldNotProceed //signals external database errors
    ImplementationError //signals errors in server code
)

var databaseName string = "precisely-db"
var collectionName string = "precisely-documents"

//exposing credentials is bad. Used here for ease.
var databaseURI string = "mongodb+srv://db-user:badpassword@cluster0.emhr5.mongodb.net/myFirstDatabase?retryWrites=true&w=majority"

//use these for calls to MongoDB database
var mongoClient *mongo.Client
var mongoCollection *mongo.Collection

func initMongoDB() {
    var initErr error
    mongoClient, initErr = mongo.NewClient(options.Client().ApplyURI(databaseURI))

    if initErr != nil {
        log.Fatal("Error setting up MongoDB client")
    }

    ctx, _ := context.WithTimeout(context.Background(), 10 * time.Second)
    initErr = mongoClient.Connect(ctx)

    if initErr != nil {
        log.Fatal("Error connecting to MongoDB database using URI: " + databaseURI)
    }

    mongoCollection = mongoClient.Database(databaseName).Collection(collectionName)
}

func init() {
    initMongoDB()
}

func destruct() { //called by defer in main file
    ctx, _ := context.WithTimeout(context.Background(), 10 * time.Second)
    mongoClient.Disconnect(ctx)
}

func getDocument(id int) (DocumentStatus, *Document) {
  	var document Document

  	findErr := mongoCollection.FindOne(
  		context.TODO(),
  		bson.D{{"id", id}},
  		options.FindOne(),
  	).Decode(&document)

  	if findErr != nil {
  		if findErr == mongo.ErrNoDocuments {
          return NotFound, nil
      }

      return CouldNotProceed, nil
  	}

    return OK, &document
}


func getDocuments() (DocumentStatus, []Document) {
    opts := options.Find().SetSort(bson.D{{"id", 1}}) //sort results by id. 1 = ascending order
    cursor, findErr := mongoCollection.Find(context.TODO(), bson.D{}, opts)

	  if findErr != nil {
      return CouldNotProceed, nil
	  }

    var documents []Document
    findErr = cursor.All(context.TODO(), &documents)

	  if findErr != nil {
      return CouldNotProceed, nil
	  }

    return OK, documents
}

/* query MongoDB for the current highest id, then add one. Should ideally be
done automatically by MongoDB upon insert. */
func getNewId() (int, error) {
    var document Document

    findErr := mongoCollection.FindOne(
      context.TODO(),
      bson.D{},
      options.FindOne().SetSort(bson.D{{"id", -1}}), //sort results by id. -1 = descending order
    ).Decode(&document)

    if findErr != nil {
  		if findErr == mongo.ErrNoDocuments { //db collection is empty
          return 0, nil
      }

      return -1, findErr
  	}

    return *document.ID + 1, nil
}

/* use this when wanting to call getDocument as a part of other requests and the
id is known to exist. It features practical handling of the getDocument outcomes */
func internalGetDocument(id int) (DocumentStatus, *Document){
    status, document := getDocument(id)

    switch status {
    case OK:
      return OK, document
    case CouldNotProceed:
      return CouldNotProceed, nil
    case NotFound:
      fallthrough
    default: //unexpected state
      return ImplementationError, nil
    }
}

func createDocument(document Document) (DocumentStatus, *Document) {
    newId, idErr := getNewId()

    if idErr != nil {
        return CouldNotProceed, nil
    }

    //set and overwrite potential existing id
    document.ID = new(int)
    *document.ID = newId

    _ , insertErr := mongoCollection.InsertOne(context.Background(), document)

    if insertErr != nil {
      return CouldNotProceed, nil
    }

    return internalGetDocument(newId)
}

func toStrippedMap(document Document) (map[string]interface{}, error) {
    /* following steps are used to achieve a map from a document, stripped
    from nil values (otherwise the db update will write nil values). These
    steps utilize the omitempty keyword in Document struct */

    //this step strips nil values
    serialDocument, serialErr := json.Marshal(document)

    if serialErr != nil {
        return nil, serialErr
    }

    //flatten the nested part of a document
    flatDocument, flatErr := flatten.FlattenString(string(serialDocument), "", flatten.DotStyle)

    if flatErr != nil {
        return nil, flatErr
    }

    strippedMap := make(map[string]interface{})
    json.Unmarshal([]byte(string(flatDocument)), &strippedMap)

    return strippedMap, nil
}


/* patchDocument is incomplete, i.e. some values are nil. These values will
not be updated, but any declared values will. ID must be set. */
func updateDocument(patchDocument Document) (DocumentStatus, *Document) {
    if patchDocument.ID == nil {
        return ImplementationError, nil
    }

    id := *patchDocument.ID

    strippedMap, stripErr := toStrippedMap(patchDocument)

    if stripErr != nil {
        return ImplementationError, nil
    }

    opts := options.Update().SetUpsert(false) //no upserts, keeping it strict
  	filter := bson.D{{"id", id}}
  	update := bson.D{{"$set", strippedMap}}

  	result, updateErr := mongoCollection.UpdateOne(context.TODO(), filter, update, opts)

  	if updateErr != nil {
        return CouldNotProceed, nil
  	}

  	if result.MatchedCount == 0 {
    		return NotFound, nil
  	}

    return internalGetDocument(id)
}

func deleteDocument(id int) (DocumentStatus) {
    result, deleteErr := mongoCollection.DeleteOne(context.TODO(), bson.M{"id": id})

    if deleteErr != nil {
        return CouldNotProceed
    }

    if result.DeletedCount == 0 {
        return NotFound
    }

    return OK
}
