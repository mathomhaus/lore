package bge

import _ "embed"

// modelBytes is the int8-quantized BAAI/bge-small-en-v1.5 ONNX model,
// embedded at build time. The model file is ~34 MB.
//
//go:embed model/model.onnx
var modelBytes []byte

// vocabBytes is the WordPiece vocabulary for bge-small-en-v1.5.
//
//go:embed model/vocab.txt
var vocabBytes []byte
