package cmd

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/qluvio/avpipe"
	"github.com/spf13/cobra"
)

//Implement AVPipeInputOpener
type avcmdInputOpener struct {
	url string
}

func (io *avcmdInputOpener) Open(fd int64, url string) (avpipe.InputHandler, error) {
	if len(url) >= 4 && url[0:4] == "rtmp" {
		return &avcmdInput{url: url}, nil
	}

	f, err := os.OpenFile(url, os.O_RDONLY, 0755)
	if err != nil {
		return nil, err
	}

	io.url = url
	etxInput := &avcmdInput{
		file: f,
		url:  url,
	}

	return etxInput, nil
}

// Implement InputHandler
type avcmdInput struct {
	url  string
	file *os.File // Input file
}

func (i *avcmdInput) Read(buf []byte) (int, error) {
	if i.url[0:4] == "rtmp" {
		return 0, nil
	}
	n, err := i.file.Read(buf)
	if err == io.EOF {
		return 0, nil
	}
	return n, err
}

func (i *avcmdInput) Seek(offset int64, whence int) (int64, error) {
	if i.url[0:4] == "rtmp" {
		return 0, nil
	}

	n, err := i.file.Seek(int64(offset), int(whence))
	return n, err
}

func (i *avcmdInput) Close() error {
	if i.url[0:4] == "rtmp" {
		return nil
	}

	err := i.file.Close()
	return err
}

func (i *avcmdInput) Size() int64 {
	fi, err := i.file.Stat()
	if err != nil {
		return -1
	}
	return fi.Size()
}

func (i *avcmdInput) Stat(statType avpipe.AVStatType, statArgs interface{}) error {
	switch statType {
	case avpipe.AV_IN_STAT_BYTES_READ:
		readOffset := statArgs.(*uint64)
		log.Info("AVCMD", "stat read offset", *readOffset)
	}

	return nil
}

//Implement AVPipeOutputOpener
type avcmdOutputOpener struct {
	dir string
}

func (oo *avcmdOutputOpener) Open(h, fd int64, stream_index, seg_index int, out_type avpipe.AVType) (avpipe.OutputHandler, error) {
	var filename string
	dir := fmt.Sprintf("%s/O%d", oo.dir, h)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.Mkdir(dir, 0755)
	}

	switch out_type {
	case avpipe.DASHVideoInit:
		fallthrough
	case avpipe.DASHAudioInit:
		filename = fmt.Sprintf("./%s/init-stream%d.m4s", dir, stream_index)
	case avpipe.DASHManifest:
		filename = fmt.Sprintf("./%s/dash.mpd", dir)
	case avpipe.DASHVideoSegment:
		fallthrough
	case avpipe.DASHAudioSegment:
		filename = fmt.Sprintf("./%s/chunk-stream%d-%05d.m4s", dir, stream_index, seg_index)
	case avpipe.HLSMasterM3U:
		filename = fmt.Sprintf("./%s/master.m3u8", dir)
	case avpipe.HLSVideoM3U:
		fallthrough
	case avpipe.HLSAudioM3U:
		filename = fmt.Sprintf("./%s/media_%d.m3u8", dir, stream_index)
	case avpipe.AES128Key:
		filename = fmt.Sprintf("./%s/key.bin", dir)
	case avpipe.MP4Stream:
		filename = fmt.Sprintf("%s/mp4-stream.mp4", dir)
	case avpipe.FMP4Stream:
		filename = fmt.Sprintf("%s/fmp4-stream.mp4", dir)
	case avpipe.MP4Segment:
		filename = fmt.Sprintf("%s/segment%d-%05d.mp4", dir, stream_index, seg_index)
	case avpipe.FMP4VideoSegment:
		filename = fmt.Sprintf("%s/fmp4-vsegment%d-%05d.mp4", dir, stream_index, seg_index)
	case avpipe.FMP4AudioSegment:
		filename = fmt.Sprintf("%s/fmp4-asegment%d-%05d.mp4", dir, stream_index, seg_index)
	}

	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}

	oh := &avcmdOutput{
		url:          filename,
		stream_index: stream_index,
		seg_index:    seg_index,
		file:         f}

	return oh, nil
}

// Implement OutputHandler
type avcmdOutput struct {
	url          string
	stream_index int
	seg_index    int
	file         *os.File
}

func (o *avcmdOutput) Write(buf []byte) (int, error) {
	n, err := o.file.Write(buf)
	return n, err
}

func (o *avcmdOutput) Seek(offset int64, whence int) (int64, error) {
	n, err := o.file.Seek(offset, whence)
	return n, err
}

func (o *avcmdOutput) Close() error {
	err := o.file.Close()
	return err
}

func (o *avcmdOutput) Stat(statType avpipe.AVStatType, statArgs interface{}) error {
	switch statType {
	case avpipe.AV_OUT_STAT_BYTES_WRITTEN:
		writeOffset := statArgs.(*uint64)
		log.Info("AVCMD", "STAT, write offset", *writeOffset)
	case avpipe.AV_OUT_STAT_DECODING_START_PTS:
		startPTS := statArgs.(*uint64)
		log.Info("AVCMD", "STAT, startPTS", *startPTS)
	case avpipe.AV_OUT_STAT_ENCODING_END_PTS:
		endPTS := statArgs.(*uint64)
		log.Info("AVCMD", "STAT, endPTS", *endPTS)

	}

	return nil
}

func getAudioIndexes(params *avpipe.TxParams, audioIndexes string) (err error) {
	params.NumAudio = 0
	if len(audioIndexes) == 0 {
		return
	}

	indexes := strings.Split(audioIndexes, ",")
	for _, indexStr := range indexes {
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			return fmt.Errorf("Invalid audio indexes")
		}
		params.AudioIndex[params.NumAudio] = int32(index)
		params.NumAudio++
	}

	return nil
}

func InitTranscode(cmdRoot *cobra.Command) error {
	cmdTranscode := &cobra.Command{
		Use:   "transcode",
		Short: "Transcode a media file",
		Long:  "Transcode a media file and produce segment files in O directory",
		//Args:  cobra.ExactArgs(1),
		RunE: doTranscode,
	}

	cmdRoot.AddCommand(cmdTranscode)

	cmdTranscode.PersistentFlags().StringP("filename", "f", "", "(mandatory) filename to be transcoded.")
	cmdTranscode.PersistentFlags().BoolP("bypass", "b", false, "bypass transcoding.")
	cmdTranscode.PersistentFlags().BoolP("skip-decoding", "", false, "skip decoding when start-time-ts is set.")
	cmdTranscode.PersistentFlags().BoolP("listen", "", false, "listen mode for RTMP.")
	cmdTranscode.PersistentFlags().Int32P("threads", "t", 1, "transcoding threads.")
	cmdTranscode.PersistentFlags().StringP("audio-index", "", "", "the indexes of audio stream (comma separated).")
	cmdTranscode.PersistentFlags().Int32P("gpu-index", "", -1, "Use the GPU with specified index for transcoding (export CUDA_DEVICE_ORDER=PCI_BUS_ID would use smi index).")
	cmdTranscode.PersistentFlags().BoolP("audio-fill-gap", "", false, "fill audio gap when encoder is aac and decoder is mpegts")
	cmdTranscode.PersistentFlags().Int32P("sync-audio-to-stream-id", "", -1, "sync audio to video iframe of specific stream-id when input stream is mpegts")
	cmdTranscode.PersistentFlags().StringP("encoder", "e", "libx264", "encoder codec, default is 'libx264', can be: 'libx264', 'libx265', 'h264_nvenc', 'h264_videotoolbox'.")
	cmdTranscode.PersistentFlags().StringP("audio-encoder", "", "aac", "audio encoder, default is 'aac', can be: 'aac', 'ac3', 'mp2', 'mp3'.")
	cmdTranscode.PersistentFlags().StringP("decoder", "d", "", "video decoder, default is 'h264', can be: 'h264', 'h264_cuvid', 'jpeg2000', 'hevc'.")
	cmdTranscode.PersistentFlags().StringP("audio-decoder", "", "", "audio decoder, default is '' and will be automatically chosen.")
	cmdTranscode.PersistentFlags().StringP("format", "", "dash", "package format, can be 'dash', 'hls', 'mp4', 'fmp4', 'segment' or 'fmp4-segment'.")
	cmdTranscode.PersistentFlags().StringP("filter-descriptor", "", "", " Audio filter descriptor the same as ffmpeg format")
	cmdTranscode.PersistentFlags().Int32P("force-keyint", "", 0, "force IDR key frame in this interval.")
	cmdTranscode.PersistentFlags().BoolP("equal-fduration", "", false, "force equal frame duration. Must be 0 or 1 and only valid for 'fmp4-segment' format.")
	cmdTranscode.PersistentFlags().StringP("tx-type", "", "", "transcoding type, can be 'all', 'video', 'audio', 'audio-join', or 'audio-merge'.")
	cmdTranscode.PersistentFlags().Int32P("crf", "", 23, "mutually exclusive with video-bitrate.")
	cmdTranscode.PersistentFlags().StringP("preset", "", "medium", "Preset string to determine compression speed, can be: 'ultrafast', 'superfast', 'veryfast', 'faster', 'fast', 'medium', 'slow', 'slower', 'veryslow'")
	cmdTranscode.PersistentFlags().Int64P("start-time-ts", "", 0, "offset to start transcoding")
	cmdTranscode.PersistentFlags().Int32P("stream-id", "", -1, "if it is valid it will be used to transcode elementary stream with that stream-id")
	cmdTranscode.PersistentFlags().Int64P("start-pts", "", 0, "starting PTS for output.")
	cmdTranscode.PersistentFlags().Int32P("sample-rate", "", -1, "For aac output sample rate is set to input sample rate and this parameter is ignored.")
	cmdTranscode.PersistentFlags().Int32P("start-segment", "", 1, "start segment number >= 1.")
	cmdTranscode.PersistentFlags().Int32P("start-frag-index", "", 1, "start fragment index >= 1.")
	cmdTranscode.PersistentFlags().Int32P("video-bitrate", "", -1, "output video bitrate, mutually exclusive with crf.")
	cmdTranscode.PersistentFlags().Int32P("audio-bitrate", "", 128000, "output audio bitrate.")
	cmdTranscode.PersistentFlags().Int32P("rc-max-rate", "", -1, "mandatory, positive integer.")
	cmdTranscode.PersistentFlags().Int32P("enc-height", "", -1, "default -1 means use source height.")
	cmdTranscode.PersistentFlags().Int32P("enc-width", "", -1, "default -1 means use source width.")
	cmdTranscode.PersistentFlags().Int64P("duration-ts", "", -1, "default -1 means entire stream.")
	cmdTranscode.PersistentFlags().Int64P("audio-seg-duration-ts", "", 0, "(mandatory if format is not 'segment' and transcoding audio) audio segment duration time base (positive integer).")
	cmdTranscode.PersistentFlags().Int64P("video-seg-duration-ts", "", 0, "(mandatory if format is not 'segment' and transcoding video) video segment duration time base (positive integer).")
	cmdTranscode.PersistentFlags().StringP("seg-duration", "", "30", "(mandatory if format is 'segment') segment duration seconds (positive integer), default is 30.")
	cmdTranscode.PersistentFlags().Int32P("seg-duration-fr", "", 0, "(mandatory if format is not 'segment') segment duration frame (positive integer).")
	cmdTranscode.PersistentFlags().String("crypt-iv", "", "128-bit AES IV, as 32 char hex.")
	cmdTranscode.PersistentFlags().String("crypt-key", "", "128-bit AES key, as 32 char hex.")
	cmdTranscode.PersistentFlags().String("crypt-kid", "", "16-byte key ID, as 32 char hex.")
	cmdTranscode.PersistentFlags().String("crypt-key-url", "", "specify a key URL in the manifest.")
	cmdTranscode.PersistentFlags().String("crypt-scheme", "none", "encryption scheme, default is 'none', can be: 'aes-128', 'cbc1', 'cbcs', 'cenc', 'cens'.")
	cmdTranscode.PersistentFlags().String("wm-text", "", "add text to the watermark display.")
	cmdTranscode.PersistentFlags().String("wm-timecode", "", "add timecode watermark to each frame.")
	cmdTranscode.PersistentFlags().Float32("wm-timecode-rate", -1, "Watermark timecode frame rate.")
	cmdTranscode.PersistentFlags().String("wm-xloc", "", "the xLoc of the watermark as specified by a fraction of width.")
	cmdTranscode.PersistentFlags().String("wm-yloc", "", "the yLoc of the watermark as specified by a fraction of height.")
	cmdTranscode.PersistentFlags().Float32("wm-relative-size", 0.05, "font/shadow relative size based on frame height.")
	cmdTranscode.PersistentFlags().String("wm-color", "black", "watermark font color.")
	cmdTranscode.PersistentFlags().BoolP("wm-shadow", "", true, "watermarking with shadow.")
	cmdTranscode.PersistentFlags().String("wm-shadow-color", "white", "watermark shadow color.")
	cmdTranscode.PersistentFlags().String("wm-overlay", "", "watermark overlay image file.")
	cmdTranscode.PersistentFlags().String("wm-overlay-type", "png", "watermark overlay image file type, can be 'png', 'jpg', 'gif'.")
	cmdTranscode.PersistentFlags().String("max-cll", "", "Maximum Content Light Level and Maximum Frame Average Light Level, only valid if encoder is libx265.")
	cmdTranscode.PersistentFlags().String("master-display", "", "Master display, only valid if encoder is libx265.")
	cmdTranscode.PersistentFlags().Int32("bitdepth", 8, "Refers to number of colors each pixel can have, can be 8, 10, 12.")

	return nil
}

func doTranscode(cmd *cobra.Command, args []string) error {

	filename := cmd.Flag("filename").Value.String()
	if len(filename) == 0 {
		return fmt.Errorf("Filename is needed after -f")
	}

	bypass, err := cmd.Flags().GetBool("bypass")
	if err != nil {
		return fmt.Errorf("Invalid bypass flag")
	}

	skipDecoding, err := cmd.Flags().GetBool("skip-decoding")
	if err != nil {
		return fmt.Errorf("Invalid skip-decoding flag")
	}

	listen, err := cmd.Flags().GetBool("listen")
	if err != nil {
		return fmt.Errorf("Invalid listen flag")
	}

	forceEqualFrameDuration, err := cmd.Flags().GetBool("equal-fduration")
	if err != nil {
		return fmt.Errorf("Invalid equal-fduration flag")
	}

	nThreads, err := cmd.Flags().GetInt32("threads")
	if err != nil {
		return fmt.Errorf("Invalid threads flag")
	}

	audioIndex := cmd.Flag("audio-index").Value.String()

	gpuIndex, err := cmd.Flags().GetInt32("gpu-index")
	if err != nil {
		return fmt.Errorf("Invalid gpu index flag")
	}

	audioFillGap, err := cmd.Flags().GetBool("audio-fill-gap")
	if err != nil {
		return fmt.Errorf("Invalid audio-fill-gap flag")
	}

	syncAudioToStreamId, err := cmd.Flags().GetInt32("sync-audio-to-stream-id")
	if err != nil {
		return fmt.Errorf("Invalid sync-audio-to-stream-id flag")
	}

	encoder := cmd.Flag("encoder").Value.String()
	if len(encoder) == 0 {
		return fmt.Errorf("Encoder is needed after -e")
	}

	audioEncoder := cmd.Flag("audio-encoder").Value.String()
	if len(audioEncoder) == 0 {
		return fmt.Errorf("Audio encoder is missing")
	}

	decoder := cmd.Flag("decoder").Value.String()
	audioDecoder := cmd.Flag("audio-decoder").Value.String()

	format := cmd.Flag("format").Value.String()
	if format != "dash" && format != "hls" && format != "mp4" && format != "fmp4" && format != "segment" && format != "fmp4-segment" {
		return fmt.Errorf("Pakage format is not valid, can be 'dash', 'hls', 'mp4', 'fmp4', 'segment', or 'fmp4-segment'")
	}

	filterDescriptor := cmd.Flag("filter-descriptor").Value.String()

	watermarkTimecode := cmd.Flag("wm-timecode").Value.String()
	watermarkTimecodeRate, _ := cmd.Flags().GetFloat32("wm-timecode-rate")
	if len(watermarkTimecode) > 0 && watermarkTimecodeRate <= 0 {
		return fmt.Errorf("Watermark timecode rate is needed")
	}
	watermarkText := cmd.Flag("wm-text").Value.String()
	watermarkXloc := cmd.Flag("wm-xloc").Value.String()
	watermarkYloc := cmd.Flag("wm-yloc").Value.String()
	watermarkFontColor := cmd.Flag("wm-color").Value.String()
	watermarkRelativeSize, _ := cmd.Flags().GetFloat32("wm-relative-size")
	watermarkShadow, _ := cmd.Flags().GetBool("watermark-shadow")
	watermarkShadowColor := cmd.Flag("wm-shadow-color").Value.String()
	watermarkOverlay := cmd.Flag("wm-overlay").Value.String()

	var watermarkOverlayType avpipe.ImageType
	watermarkOverlayTypeStr := cmd.Flag("wm-overlay-type").Value.String()
	switch watermarkOverlayTypeStr {
	case "png":
		fallthrough
	case "PNG":
		watermarkOverlayType = avpipe.PngImage
	case "jpg":
		fallthrough
	case "JPG":
		watermarkOverlayType = avpipe.JpgImage
	case "gif":
		fallthrough
	case "GIF":
		watermarkOverlayType = avpipe.GifImage
	default:
		watermarkOverlayType = avpipe.UnknownImage
	}
	if len(watermarkOverlay) > 0 && watermarkOverlayType == avpipe.UnknownImage {
		return fmt.Errorf("Watermark overlay type is not valid, can be 'png', 'jpg', 'gif'")
	}

	var overlayImage []byte
	if len(watermarkOverlay) > 0 {
		overlayImage, err = ioutil.ReadFile(watermarkOverlay)
		if err != nil {
			return err
		}
	}

	streamId, err := cmd.Flags().GetInt32("stream-id")
	if err != nil {
		return fmt.Errorf("stream-id is not valid, must be >= 0")
	}

	txTypeStr := cmd.Flag("tx-type").Value.String()
	if streamId < 0 && txTypeStr != "all" &&
		txTypeStr != "video" &&
		txTypeStr != "audio" &&
		txTypeStr != "audio-join" &&
		txTypeStr != "audio-pan" {
		return fmt.Errorf("Transcoding type is not valid, with no stream-id can be 'all', 'video', 'audio', 'audio-join', or 'audio-pan'")
	}
	txType := avpipe.TxTypeFromString(txTypeStr)
	if txType == avpipe.TxAudio && len(encoder) == 0 {
		encoder = "aac"
	}

	maxCLL := cmd.Flag("max-cll").Value.String()
	masterDisplay := cmd.Flag("master-display").Value.String()
	bitDepth, err := cmd.Flags().GetInt32("bitdepth")
	if err != nil {
		return fmt.Errorf("bitdepth is not valid, should be 8, 10, or 12")
	}

	crf, err := cmd.Flags().GetInt32("crf")
	if err != nil || crf < 0 || crf > 51 {
		return fmt.Errorf("crf is not valid, should be in 0..51")
	}

	preset := cmd.Flag("preset").Value.String()
	if preset != "ultrafast" && preset != "superfast" && preset != "veryfast" && preset != "faster" &&
		preset != "fast" && preset != "medium" && preset != "slow" && preset != "slower" && preset != "veryslow" {
		return fmt.Errorf("preset is not valid, should be one of: 'ultrafast', 'superfast', 'veryfast', 'faster', 'fast', 'medium', 'slow', 'slower', 'veryslow'")
	}

	startTimeTs, err := cmd.Flags().GetInt64("start-time-ts")
	if err != nil {
		return fmt.Errorf("start-time-ts is not valid")
	}

	startPts, err := cmd.Flags().GetInt64("start-pts")
	if err != nil || startPts < 0 {
		return fmt.Errorf("start-pts is not valid, must be >=0")
	}

	sampleRate, err := cmd.Flags().GetInt32("sample-rate")
	if err != nil {
		return fmt.Errorf("sample-rate is not valid")
	}

	startSegment, err := cmd.Flags().GetInt32("start-segment")
	if err != nil {
		return fmt.Errorf("start-segment is not valid")
	}

	forceKeyInterval, err := cmd.Flags().GetInt32("force-keyint")
	if err != nil {
		return fmt.Errorf("force-keyint is not valid")
	}

	startFragmentIndex, err := cmd.Flags().GetInt32("start-frag-index")
	if err != nil {
		return fmt.Errorf("start-frag-index is not valid")
	}

	videoBitrate, err := cmd.Flags().GetInt32("video-bitrate")
	if err != nil {
		return fmt.Errorf("video-bitrate is not valid")
	}

	audioBitrate, err := cmd.Flags().GetInt32("audio-bitrate")
	if err != nil {
		return fmt.Errorf("audio-bitrate is not valid")
	}

	rcMaxRate, err := cmd.Flags().GetInt32("rc-max-rate")
	if err != nil {
		return fmt.Errorf("rc-max-rate is not valid")
	}

	encHeight, err := cmd.Flags().GetInt32("enc-height")
	if err != nil {
		return fmt.Errorf("enc-height is not valid")
	}

	encWidth, err := cmd.Flags().GetInt32("enc-width")
	if err != nil {
		return fmt.Errorf("enc-width is not valid")
	}

	durationTs, err := cmd.Flags().GetInt64("duration-ts")
	if err != nil {
		return fmt.Errorf("Duration ts is not valid")
	}

	audioSegDurationTs, err := cmd.Flags().GetInt64("audio-seg-duration-ts")
	if err != nil ||
		(format != "segment" && format != "fmp4-segment" &&
			audioSegDurationTs == 0 &&
			(txType == avpipe.TxAll || txType == avpipe.TxAudio ||
				txType == avpipe.TxAudioJoin || txType == avpipe.TxAudioMerge)) {
		return fmt.Errorf("Audio seg duration ts is not valid")
	}

	videoSegDurationTs, err := cmd.Flags().GetInt64("video-seg-duration-ts")
	if err != nil || (format != "segment" && format != "fmp4-segment" &&
		videoSegDurationTs == 0 && (txType == avpipe.TxAll || txType == avpipe.TxVideo)) {
		return fmt.Errorf("Video seg duration ts is not valid")
	}

	segDuration := cmd.Flag("seg-duration").Value.String()
	if format == "segment" && len(segDuration) == 0 {
		return fmt.Errorf("Seg duration ts is not valid")
	}

	crfStr := strconv.Itoa(int(crf))
	startSegmentStr := strconv.Itoa(int(startSegment))

	cryptScheme := avpipe.CryptNone
	val := cmd.Flag("crypt-scheme").Value.String()
	if len(val) > 0 {
		switch val {
		case "aes-128":
			cryptScheme = avpipe.CryptAES128
		case "cenc":
			cryptScheme = avpipe.CryptCENC
		case "cbc1":
			cryptScheme = avpipe.CryptCBC1
		case "cens":
			cryptScheme = avpipe.CryptCENS
		case "cbcs":
			cryptScheme = avpipe.CryptCBCS
		case "none":
			break
		default:
			return fmt.Errorf("Invalid crypt-scheme: %s", val)
		}
	}
	cryptIV := cmd.Flag("crypt-iv").Value.String()
	cryptKey := cmd.Flag("crypt-key").Value.String()
	cryptKID := cmd.Flag("crypt-kid").Value.String()
	cryptKeyURL := cmd.Flag("crypt-key-url").Value.String()

	dir := "O"
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.Mkdir(dir, 0755)
	}

	params := &avpipe.TxParams{
		BypassTranscoding:     bypass,
		Format:                format,
		StartTimeTs:           startTimeTs,
		StartPts:              startPts,
		DurationTs:            durationTs,
		StartSegmentStr:       startSegmentStr,
		StartFragmentIndex:    startFragmentIndex,
		VideoBitrate:          videoBitrate,
		AudioBitrate:          audioBitrate,
		SampleRate:            sampleRate,
		CrfStr:                crfStr,
		Preset:                preset,
		AudioSegDurationTs:    audioSegDurationTs,
		VideoSegDurationTs:    videoSegDurationTs,
		SegDuration:           segDuration,
		Ecodec:                encoder,
		Ecodec2:               audioEncoder,
		Dcodec:                decoder,
		Dcodec2:               audioDecoder,
		EncHeight:             encHeight, // -1 means use source height, other values 2160, 720
		EncWidth:              encWidth,  // -1 means use source width, other values 3840, 1280
		CryptIV:               cryptIV,
		CryptKey:              cryptKey,
		CryptKID:              cryptKID,
		CryptKeyURL:           cryptKeyURL,
		CryptScheme:           cryptScheme,
		TxType:                txType,
		WatermarkTimecode:     watermarkTimecode,
		WatermarkTimecodeRate: watermarkTimecodeRate,
		WatermarkText:         watermarkText,
		WatermarkXLoc:         watermarkXloc,
		WatermarkYLoc:         watermarkYloc,
		WatermarkRelativeSize: watermarkRelativeSize,
		WatermarkFontColor:    watermarkFontColor,
		WatermarkShadow:       watermarkShadow,
		WatermarkShadowColor:  watermarkShadowColor,
		WatermarkOverlay:      string(overlayImage),
		WatermarkOverlayType:  watermarkOverlayType,
		ForceKeyInt:           forceKeyInterval,
		RcMaxRate:             rcMaxRate,
		RcBufferSize:          4500000,
		GPUIndex:              gpuIndex,
		MaxCLL:                maxCLL,
		MasterDisplay:         masterDisplay,
		BitDepth:              bitDepth,
		ForceEqualFDuration:   forceEqualFrameDuration,
		AudioFillGap:          audioFillGap,
		SyncAudioToStreamId:   int(syncAudioToStreamId),
		StreamId:              streamId,
		Listen:                listen,
		FilterDescriptor:      filterDescriptor,
		SkipDecoding:          skipDecoding,
	}

	err = getAudioIndexes(params, audioIndex)
	if err != nil {
		return err
	}

	params.WatermarkOverlayLen = len(params.WatermarkOverlay)

	avpipe.InitIOHandler(&avcmdInputOpener{url: filename}, &avcmdOutputOpener{dir: dir})

	done := make(chan interface{})

	for i := 0; i < int(nThreads); i++ {
		go func(params *avpipe.TxParams, filename string) {

			err := avpipe.Tx(params, filename, true)
			if err != nil {
				done <- fmt.Errorf("Failed transcoding %s", filename)
			} else {
				done <- nil
			}
		}(params, filename)
	}

	var lastError error
	for i := 0; i < int(nThreads); i++ {
		err := <-done // Wait for background goroutines to finish
		if err != nil {
			lastError = err.(error)
			fmt.Println(err)
		}
	}

	return lastError
}
