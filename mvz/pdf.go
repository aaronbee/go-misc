package main

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"time"
)

// This file implements simple writing of PDF files
// with JPEG pages.

type PDFWriter struct {
	w       io.Writer
	offset  int
	objects []int // offsets
	pages   []PDFID
	err     error

	// specific objects
	infoId PDFID
}

type PDFID int

type Length float64

const (
	INCH Length = 72 // 1 in = 72 pts
	CM   Length = 72 / 2.54
)

func NewPDFWriter(w io.Writer) (*PDFWriter, error) {
	p := &PDFWriter{w: w}
	p.print("%PDF-1.3")
	return p, p.err
}

func (p *PDFWriter) WriteInfo(title string, mtime time.Time) error {
	p.infoId, _ = p.startObj()
	p.printf("/Title (%s)", title)
	p.printf("/CreationDate (D:%s)", mtime.Format("20060102150405"))
	p.printf("/ModDate (D:%s)", mtime.Format("20060102150405"))
	p.print("/Producer (mvztopdf 1.0)")
	p.endObj()
	return p.err
}

func (p *PDFWriter) WritePage(x, y Length, data []byte) (PDFID, error) {
	id, _ := p.startObj()
	p.print("/Type /Page")
	p.printf("/MediaBox [0 0 %.2f %.2f]", x, y)
	p.printf("/CropBox [0 0 %.2f %.2f]", x, y)
	p.printf("/Contents %d 0 R", id+1)
	p.endObj()
	streamId, _ := p.writeStreamObject(data)
	if p.err == nil && streamId != id+1 {
		panic("internal error: streamId != id+1")
	}
	p.pages = append(p.pages, id)
	return id, p.err
}

const DPI = 150

func (p *PDFWriter) WriteJPEGPage(img image.Image, data []byte) (PDFID, error) {
	x := Length(img.Bounds().Dx()) / 150 * INCH
	y := Length(img.Bounds().Dy()) / 150 * INCH
	id, _ := p.startObj()
	p.print("/Type /Page")
	p.printf("/MediaBox [0 0 %.2f %.2f]", x, y)
	p.printf("/CropBox [0 0 %.2f %.2f]", x, y)
	p.printf("/Contents %d 0 R", id+1)
	p.printf("/Resources << /XObject << /I %d 0 R >> >>", id+2)
	p.endObj()
	// Postscript code
	buf := new(bytes.Buffer)
	buf.WriteString("q\n")
	fmt.Fprintf(buf, "%.2f 0 0 %.2f 0 0 cm\n", x, y)
	buf.WriteString("/I Do\n")
	buf.WriteString("Q\n")
	streamId, _ := p.writeStreamObject(buf.Bytes())
	if p.err == nil && streamId != id+1 {
		panic("internal error: streamId != id+1")
	}
	// Image
	imgId, _ := p.writeImage(img.Bounds().Dx(), img.Bounds().Dy(), data)
	if p.err == nil && imgId != id+2 {
		panic("internal error: imgId != id+2")
	}
	p.pages = append(p.pages, id)
	return id, p.err
}

func (p *PDFWriter) writeImage(w, h int, data []byte) (PDFID, error) {
	id, _ := p.startObj()
	p.print("/Type /XObject")
	p.print("/Subtype /Image")
	p.print("/Name /I")
	p.print("/Filter [ /DCTDecode ]") // for JPEG
	p.printf("/Width %d", w)
	p.printf("/Height %d", h)
	p.print("/ColorSpace /DeviceRGB")
	p.print("/BitsPerComponent 8")
	p.printf("/Length %d", len(data))
	p.print(">>") // end dict
	p.writeStream(data)
	p.endObj()
	return id, p.err
}

func (p *PDFWriter) writeStreamObject(data []byte) (PDFID, error) {
	id, _ := p.startObj()
	p.printf("/Length %d", len(data))
	p.print(">>") // end dict
	p.writeStream(data)
	p.endObj()
	return id, p.err
}

func (p *PDFWriter) writeStream(data []byte) {
	p.print("stream")
	n, err := p.w.Write(data)
	p.offset += n
	p.err = err
	p.print("endstream")
}

func (p *PDFWriter) Flush() error {
	// pages
	pagesId, _ := p.startObj()
	p.print("/Type /Pages")
	buf := new(bytes.Buffer)
	for _, page := range p.pages {
		fmt.Fprintf(buf, "%d 0 R ", page)
	}
	p.printf("/Kids [ %s]", buf.String())
	p.printf("/Count %d", len(p.pages))
	p.endObj()
	if p.err != nil {
		return p.err
	}
	// catalog
	rootId, _ := p.startObj()
	p.print("/Type /Catalog")
	p.printf("/Pages %d 0 R", pagesId)
	p.endObj()
	// xref table
	xrefOff := p.offset
	p.print("xref")
	p.printf("0 %d", len(p.objects)+1)
	p.print("0000000000 65535 f")
	for _, off := range p.objects {
		p.printf("%010d 00000 n", off)
	}
	// trailer
	const id = "deadbeef"
	p.print("trailer")
	p.print("<<")
	p.printf("/Size %d", len(p.objects)+1)
	p.printf("/Info %d 0 R", p.infoId)
	p.printf("/Root %d 0 R", rootId)
	p.printf("/ID [<%s> <%s>]", id, id)
	p.print(">>")
	// end
	p.print("startxref")
	p.printf("%d", xrefOff)
	p.print("%%EOF")
	return p.err
}

// Utility functions

var nl = []byte{'\n'}

func (p *PDFWriter) print(s string) error {
	n, err := io.WriteString(p.w, s)
	p.offset += n
	if err != nil {
		p.err = err
		return err
	}
	_, p.err = p.w.Write(nl)
	p.offset++
	return p.err
}

func (p *PDFWriter) printf(format string, args ...interface{}) error {
	n, err := fmt.Fprintf(p.w, format, args...)
	p.offset += n
	if err != nil {
		p.err = err
		return err
	}
	_, p.err = p.w.Write(nl)
	p.offset++
	return p.err
}

func (p *PDFWriter) startObj() (PDFID, error) {
	p.objects = append(p.objects, p.offset)
	id := PDFID(len(p.objects))
	p.printf("%d 0 obj", id)
	p.print("<<")
	return id, p.err
}

func (p *PDFWriter) endObj() error {
	p.print(">>")
	p.print("endobj")
	return p.err
}

func (p *PDFWriter) intObj(n int) (PDFID, error) {
	p.objects = append(p.objects, p.offset)
	id := PDFID(len(p.objects))
	p.printf("%d 0 obj", id)
	p.printf("%d", n)
	p.print("endobj")
	return id, p.err
}
