//go:build !gocv_specific_modules || (gocv_specific_modules && gocv_objdetect)

#include "objdetect.h"
















// QRCodeDetector

QRCodeDetector QRCodeDetector_New() {
    try {
        return new cv::QRCodeDetector();
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return NULL;
    }
}

void QRCodeDetector_Close(QRCodeDetector qr) {
    delete qr;
}

const char* QRCodeDetector_DetectAndDecode(QRCodeDetector qr, Mat input,Mat points,Mat straight_qrcode) {
    try {
        cv::String *str = new cv::String(qr->detectAndDecode(*input,*points,*straight_qrcode)); 
        return str->c_str();
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return "";
    }
}

bool QRCodeDetector_Detect(QRCodeDetector qr, Mat input,Mat points) {
    try {
        return qr->detect(*input,*points); 
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return false;
    }
}

const char* QRCodeDetector_Decode(QRCodeDetector qr, Mat input,Mat inputPoints,Mat straight_qrcode) {
    try {
        cv::String *str = new cv::String(qr->detectAndDecode(*input,*inputPoints,*straight_qrcode)); 
        return str->c_str();
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return "";
    }
}

// QRCodeDetectorAruco
//
// Aruco-based QR localization (cv::QRCodeDetectorAruco, core objdetect module).
// Same GraphicalCodeDetector API as QRCodeDetector, so callers swap only the
// constructor.

QRCodeDetectorAruco QRCodeDetectorAruco_New() {
    try {
        return new cv::QRCodeDetectorAruco();
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return NULL;
    }
}

void QRCodeDetectorAruco_Close(QRCodeDetectorAruco qr) {
    delete qr;
}

// Returns a pointer into a thread_local buffer: valid until this thread's next
// call. cgo's C.GoString copies before returning to Go, so that is long enough.
// (Unlike QRCodeDetector_DetectAndDecode above, this does not leak a cv::String
// per call.)
const char* QRCodeDetectorAruco_DetectAndDecode(QRCodeDetectorAruco qr, Mat input, Mat points, Mat straight_qrcode) {
    try {
        static thread_local std::string buf;
        buf = qr->detectAndDecode(*input, *points, *straight_qrcode);
        return buf.c_str();
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return "";
    }
}

bool QRCodeDetector_DetectMulti(QRCodeDetector qr, Mat input, Mat points) {
    try {
        return qr->detectMulti(*input,*points);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return false;
    }
}

bool QRCodeDetector_DetectAndDecodeMulti(QRCodeDetector qr, Mat input, CStrings* decoded, Mat points, struct Mats* qrCodes) {
    try {
        std::vector<cv::String> decodedCodes;
        std::vector<cv::Mat> straightQrCodes;
        bool res = qr->detectAndDecodeMulti(*input, decodedCodes, *points, straightQrCodes);
        if (!res) {
            return res;
        }
    
        qrCodes->mats = new Mat[straightQrCodes.size()];
        qrCodes->length = straightQrCodes.size();
        for (size_t i = 0; i < straightQrCodes.size(); i++) {
            qrCodes->mats[i] = new cv::Mat(straightQrCodes[i]);
        }
    
        const char **strs = new const char*[decodedCodes.size()];
        for (size_t i = 0; i < decodedCodes.size(); ++i) {
            strs[i] = decodedCodes[i].c_str();
        }
        decoded->length = decodedCodes.size();
        decoded->strs = strs;
        return res;
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return false;
    }
}

FaceDetectorYN FaceDetectorYN_Create(const char* model, const char* config, Size size) {
    try {
        cv::String smodel = cv::String(model);
        cv::String sconfig = cv::String(config);
        cv::Size   ssize = cv::Size(size.width, size.height);
    
        return new cv::Ptr<cv::FaceDetectorYN>(cv::FaceDetectorYN::create(smodel, sconfig, ssize));
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return NULL;
    }
}

FaceDetectorYN FaceDetectorYN_Create_WithParams(const char* model, const char* config, Size size, float score_threshold, float nms_threshold, int top_k, int backend_id, int target_id) {
    try {
        cv::String smodel = cv::String(model);
        cv::String sconfig = cv::String(config);
        cv::Size   ssize = cv::Size(size.width, size.height);
    
        return new cv::Ptr<cv::FaceDetectorYN>(cv::FaceDetectorYN::create(smodel, sconfig, ssize, score_threshold, nms_threshold, top_k, backend_id, target_id));
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return NULL;
    }
}

FaceDetectorYN FaceDetectorYN_Create_FromBytes(const char* framework, void* bufferModel, int model_size, void* bufferConfig, int config_size, Size size) {
    try {
        cv::String sframework = cv::String(framework);
        cv::Size   ssize = cv::Size(size.width, size.height);
        
        std::vector<uchar> bufferModelV;
        std::vector<uchar> bufferConfigV;
    
        uchar* bmv = (uchar*)bufferModel;
        uchar* bcv = (uchar*)bufferConfig;
    
    
        for(int i = 0; i < model_size; i ++) {
            bufferModelV.push_back(bmv[i]);
        }
        for(int i = 0; i < config_size; i ++) {
            bufferConfigV.push_back(bcv[i]);
        }
    
        return new cv::Ptr<cv::FaceDetectorYN>(cv::FaceDetectorYN::create(sframework, bufferModelV, bufferConfigV, ssize));
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return NULL;
    }
}

FaceDetectorYN FaceDetectorYN_Create_FromBytes_WithParams(const char* framework, void* bufferModel, int model_size, void* bufferConfig, int config_size, Size size, float score_threshold, float nms_threshold, int top_k, int backend_id, int target_id) {
    try {
        cv::String sframework = cv::String(framework);
        cv::Size   ssize = cv::Size(size.width, size.height);
        
        std::vector<uchar> bufferModelV;
        std::vector<uchar> bufferConfigV;
    
        uchar* bmv = (uchar*)bufferModel;
        uchar* bcv = (uchar*)bufferConfig;
    
        for(int i = 0; i < model_size; i ++) {
            bufferModelV.push_back(bmv[i]);
        }
        for(int i = 0; i < config_size; i ++) {
            bufferConfigV.push_back(bcv[i]);
        }
    
        return new cv::Ptr<cv::FaceDetectorYN>(cv::FaceDetectorYN::create(sframework, bufferModelV, bufferConfigV, ssize, score_threshold, nms_threshold, top_k, backend_id, target_id));
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return NULL;
    }
}

void FaceDetectorYN_Close(FaceDetectorYN fd) {
    delete fd;
}

int FaceDetectorYN_Detect(FaceDetectorYN fd, Mat image, Mat faces) {
    try {
        return (*fd)->detect(*image, *faces);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return 0;
    }
}

Size FaceDetectorYN_GetInputSize(FaceDetectorYN fd) {
    try {
        Size sz;

        cv::Size cvsz = (*fd)->getInputSize();
    
        sz.width = cvsz.width;
        sz.height = cvsz.height;
    
        return sz;
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        Size sz = {0, 0};
        return sz;
    }
}

float FaceDetectorYN_GetNMSThreshold(FaceDetectorYN fd) {
    try {
        return (*fd)->getNMSThreshold();
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return 0.0;
    }
}

float FaceDetectorYN_GetScoreThreshold(FaceDetectorYN fd) {
    try {
        return (*fd)->getScoreThreshold();
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return 0.0;
    }
}

int FaceDetectorYN_GetTopK(FaceDetectorYN fd) {
    try {
        return (*fd)->getTopK();
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return 0;
    }
}

void FaceDetectorYN_SetInputSize(FaceDetectorYN fd, Size input_size){
    try {
        cv::Size isz(input_size.width, input_size.height);
        (*fd)->setInputSize(isz);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
    }
}

void FaceDetectorYN_SetNMSThreshold(FaceDetectorYN fd, float nms_threshold){
    try {
        (*fd)->setNMSThreshold(nms_threshold);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
    }
}

void FaceDetectorYN_SetScoreThreshold(FaceDetectorYN fd, float score_threshold){
    try {
        (*fd)->setScoreThreshold(score_threshold);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
    }
}

void FaceDetectorYN_SetTopK(FaceDetectorYN fd, int top_k){
    try {
        (*fd)->setTopK(top_k);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
    }
}

FaceRecognizerSF FaceRecognizerSF_Create(const char* model, const char* config) {
    try {
        return FaceRecognizerSF_Create_WithParams(model, config, 0, 0);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return NULL;
    }
}

FaceRecognizerSF FaceRecognizerSF_Create_WithParams(const char* model, const char* config, int backend_id, int target_id) {
    try {
        return new cv::Ptr<cv::FaceRecognizerSF>(cv::FaceRecognizerSF::create(model, config, backend_id, target_id));
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return NULL;
    }
}

void FaceRecognizerSF_Close(FaceRecognizerSF fr) {
    delete fr;
}

void FaceRecognizerSF_AlignCrop(FaceRecognizerSF fr, Mat src_img, Mat face_box, Mat aligned_img) {
    try {
        (*fr)->alignCrop(*src_img, *face_box, *aligned_img);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
    }
}

void FaceRecognizerSF_Feature(FaceRecognizerSF fr, Mat aligned_img, Mat face_feature) {
    try {
        (*fr)->feature(*aligned_img, *face_feature);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
    }
}

float FaceRecognizerSF_Match(FaceRecognizerSF fr, Mat face_feature1, Mat face_feature2) {
    try {
        return FaceRecognizerSF_Match_WithParams(fr, face_feature1, face_feature2, 0);
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return 0.0;
    }
}

float FaceRecognizerSF_Match_WithParams(FaceRecognizerSF fr, Mat face_feature1, Mat face_feature2, int dis_type) {
    try {
        double rv = (*fr)->match(*face_feature1, *face_feature2, dis_type);
        return (float)rv;
    } catch(const cv::Exception& e){
        setExceptionInfo(e.code, e.what());
        return 0.0;
    }
}