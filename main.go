package main

import (
    "github.com/gin-gonic/gin"
    "net/http"
    "strconv"
)

//pointers used because of serialization, nil pointer signals no value by omitempty
type DocumentContent struct {
    Header *string `json:"header,omitempty"`
    Data   *string `json:"data,omitempty"`
}

type Document struct {
    ID       *int              `json:"id,omitempty"`
    Title    *string           `json:"title,omitempty"`
    Content  *DocumentContent  `json:"content,omitempty"`
    Signee   *string           `json:"signee,omitempty"`
}

type HttpError struct {
    Message   string  `json:"error"`
}

func main() {
    router := gin.Default()

    defer destruct() //for dbController.go

    //read single document by id
    router.GET("/documents/:id", handleGetDocument)
    //read all documents
    router.GET("/documents", handleGetDocuments)
    //create document
    router.POST("/documents", handleCreateDocument)
    //update document
    router.PATCH("/documents/:id", handleUpdateDocument)
    //delete document
    router.DELETE("/documents/:id", handleDeleteDocument)

    //start server
    router.Run("localhost:8080")
}

func getIDParam(ginCon *gin.Context) string {
    return ginCon.Param("id")
}

func toInt (str string) (int, error) {
    return strconv.Atoi(str)
}

func toString(i int) string {
    return strconv.Itoa(i)
}

func sendJsonHttpResponse(ginCon *gin.Context, httpCode int, jsonObj interface{}) {
    ginCon.IndentedJSON(httpCode, jsonObj)
}

func handleGetDocument(ginCon *gin.Context) {
    id, toIntErr := toInt(getIDParam(ginCon))

    if toIntErr != nil {
      sendJsonHttpResponse(ginCon, http.StatusBadRequest, HttpError{"requested id '" + getIDParam(ginCon) + "' is not a number"})
      return
    }

    status, document := getDocument(id)

    switch status {
    case OK:
      sendJsonHttpResponse(ginCon, http.StatusOK, document)
    case NotFound:
      sendJsonHttpResponse(ginCon, http.StatusNotFound, HttpError{"could not find document with id " + getIDParam(ginCon)})
    case CouldNotProceed:
      sendJsonHttpResponse(ginCon, http.StatusBadGateway, HttpError{"external database does not respond properly"})
    default:
      sendJsonHttpResponse(ginCon, http.StatusInternalServerError, HttpError{"unexpected server state"})
    }
}

func handleGetDocuments(ginCon *gin.Context) {
    status, documents := getDocuments()

    switch status {
    case OK:
      sendJsonHttpResponse(ginCon, http.StatusOK, documents)
    case CouldNotProceed:
      sendJsonHttpResponse(ginCon, http.StatusBadGateway, HttpError{"external database does not respond properly"})
    default:
      sendJsonHttpResponse(ginCon, http.StatusInternalServerError, HttpError{"unexpected server state"})
    }
}

//extract json object from request and try to initialize it into a document
func bindDocument(ginCon *gin.Context) (Document, error) {
    var document Document
    return document, ginCon.BindJSON(&document)
}

//check so every required field of document is set
func isCompleteDocument(document Document) bool {
    if document.Content != nil {
      return document.Title != nil && document.Content.Header != nil &&
              document.Content.Data != nil && document.Signee != nil
    } else {
        return false
    }
}

func handleCreateDocument(ginCon *gin.Context) {
    document, initErr := bindDocument(ginCon)

    if initErr != nil {
        sendJsonHttpResponse(ginCon, http.StatusBadRequest, HttpError{"illegal structure of json object"})
        return
    }

    //validate document
    if !isCompleteDocument(document) {
      sendJsonHttpResponse(ginCon, http.StatusBadRequest, HttpError{"not a valid document for creation; every field except id is needed."})
      return
    }

    status, newDocument := createDocument(document)

    switch status {
    case OK:
      sendJsonHttpResponse(ginCon, http.StatusCreated, newDocument)
    case CouldNotProceed:
      sendJsonHttpResponse(ginCon, http.StatusBadGateway, HttpError{"external database does not respond properly"})
    case ImplementationError:
      fallthrough
    default:
      sendJsonHttpResponse(ginCon, http.StatusInternalServerError, HttpError{"unexpected server state"})
    }
}

//check so that at least one value (except ID) is set
func isValidPatchDocument(document Document) bool {
    var existingContent = false

    if document.Content != nil {
        existingContent = document.Content.Header != nil || document.Content.Data != nil
    }

    return existingContent || document.Title != nil || document.Signee != nil
}

func handleUpdateDocument(ginCon *gin.Context) {
  id, toIntErr := toInt(getIDParam(ginCon))

  if toIntErr != nil {
    sendJsonHttpResponse(ginCon, http.StatusBadRequest, HttpError{"requested id '" + getIDParam(ginCon) + "' is not a number"})
    return
  }

  patchDocument, initErr := bindDocument(ginCon)

  if initErr != nil {
      sendJsonHttpResponse(ginCon, http.StatusBadRequest, HttpError{"illegal structure of json object"})
      return
  }

  //validate patch document
  if !isValidPatchDocument(patchDocument) {
    sendJsonHttpResponse(ginCon, http.StatusBadRequest, HttpError{"not a valid document for update; at least one field except id is needed."})
    return
  }

  //if both request id and document id are set, check so that they are the same
  if patchDocument.ID != nil {
      if *patchDocument.ID != id {
          sendJsonHttpResponse(ginCon, http.StatusBadRequest, HttpError{"id in request (" + toString(id) + ") does not correpsond to id in json object (" + toString(*patchDocument.ID) + ")"})
          return
      }
  } else {
      //set request id to patch document
      patchDocument.ID = new(int);
      *patchDocument.ID = id
  }

  status, updatedDocument := updateDocument(patchDocument)

  switch status {
  case OK:
    sendJsonHttpResponse(ginCon, http.StatusOK, updatedDocument)
  case CouldNotProceed:
    sendJsonHttpResponse(ginCon, http.StatusBadGateway, HttpError{"external database does not respond properly"})
  case NotFound:
    sendJsonHttpResponse(ginCon, http.StatusNotFound, HttpError{"could not find document with id " + getIDParam(ginCon)})
  case ImplementationError:
    fallthrough
  default:
    sendJsonHttpResponse(ginCon, http.StatusInternalServerError, HttpError{"unexpected server state"})
  }
}

func handleDeleteDocument(ginCon *gin.Context) {
  id, toIntErr := toInt(getIDParam(ginCon))

  if toIntErr != nil {
    sendJsonHttpResponse(ginCon, http.StatusBadRequest, HttpError{"requested id '" + getIDParam(ginCon) + "' is not a number"})
    return
  }

  status := deleteDocument(id)

  switch status {
  case OK:
    sendJsonHttpResponse(ginCon, http.StatusNoContent, nil)
  case CouldNotProceed:
    sendJsonHttpResponse(ginCon, http.StatusBadGateway, HttpError{"external database does not respond properly"})
  case NotFound:
    sendJsonHttpResponse(ginCon, http.StatusNotFound, HttpError{"could not find document with id " + getIDParam(ginCon)})
  default:
    sendJsonHttpResponse(ginCon, http.StatusInternalServerError, HttpError{"unexpected server state"})
  }
}
