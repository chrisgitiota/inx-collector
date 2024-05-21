package api

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

type RequestConstraint interface {
	RequestSubscribeBody | RequestStoreBody | RequestCreateBucket | RequestStoreSynchronizedBlock
}

type RequestSubscribeBody struct {
	Tag        string `json:"tag" validate:"required"`
	PublicKey  string `json:"publicKey"`
	Duration   string `json:"duration"`
	BucketName string `json:"bucketName"`
	WithPOI    bool   `json:"withPOI"`
}

type RequestStoreSynchronizedBlock struct {
	BlockId    string `json:"blockId" validate:"required"`
}


type RequestStoreBody struct {
	BlockId    string `json:"blockId" validate:"required"`
	BucketName string `json:"bucketName"`
	WithPOI    bool   `json:"withPOI"`
}

type RequestCreateBucket struct {
	BucketName    string `json:"bucketName" validate:"required"`
	LifecycleDays int    `json:"days"`
}

type ObjectParams struct {
	BlockId    string
	BucketName string
	WithPOI    bool
	CheckExistence bool
}

func extractRequestBody[Request RequestConstraint](request *Request, c echo.Context) error {
	reader := c.Request().Body
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	err = json.Unmarshal(body, request)
	if err != nil {
		return err
	}
	err = validator.New().Struct(request)
	if err != nil {
		return err
	}
	return nil
}

func (s Server) parseObjectInput(c echo.Context) (ObjectParams, error) {
	var params ObjectParams
	params.BlockId = strings.ToLower(c.Param(ParameterBlockID))
	params.BucketName = s.Collector.Storage.DefaultBucketName

	err := c.Request().ParseForm()
	if err != nil {
		return params, err
	}
	if c.Request().Form.Has(ParameterBucketName) {
		params.BucketName = c.QueryParam(ParameterBucketName)
	}
	if c.Request().Form.Has(ParameterWithPOI) {
		params.WithPOI, err = strconv.ParseBool(c.QueryParam(ParameterWithPOI))
		if err != nil {
			return params, err
		}
	}
	if c.Request().Form.Has(ParameterCheckExistence) {
		params.CheckExistence, err = strconv.ParseBool(c.QueryParam(ParameterCheckExistence))
		if err != nil {
			return params, err
		}
	}

	return params, nil
}
