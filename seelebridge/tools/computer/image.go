package computer

import "image"

// ScaleNearest 以最近邻方式把图像缩放到指定最大宽度；宽度不足时原样返回。
// 缩放只影响给模型查看的像素，不改变工具的坐标系。
func ScaleNearest(src *image.RGBA, maxWidth int) (*image.RGBA, float64) {
	if src == nil {
		return nil, 1
	}
	b := src.Bounds()
	if maxWidth <= 0 || b.Dx() <= maxWidth {
		return src, 1
	}
	scale := float64(maxWidth) / float64(b.Dx())
	dstW := maxWidth
	dstH := int(float64(b.Dy()) * scale)
	if dstH < 1 {
		dstH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		srcY := b.Min.Y + int(float64(y)/scale)
		if srcY >= b.Max.Y {
			srcY = b.Max.Y - 1
		}
		for x := 0; x < dstW; x++ {
			srcX := b.Min.X + int(float64(x)/scale)
			if srcX >= b.Max.X {
				srcX = b.Max.X - 1
			}
			si := src.PixOffset(srcX, srcY)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst, scale
}
