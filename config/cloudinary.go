// config/cloudinary.go
package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
)

var Cld *cloudinary.Cloudinary

func InitCloudinary() {
	var err error

	// วิธีที่ 1: รับค่าจาก CLOUDINARY_URL (แบบรวม)
	cloudinaryURL := os.Getenv("CLOUDINARY_URL")

	fmt.Printf("🔍 Checking CLOUDINARY_URL: %s\n", maskCloudinaryURL(cloudinaryURL))

	if cloudinaryURL == "" {
		log.Println("⚠️  CLOUDINARY_URL not found, using local storage")
		log.Println("💡 Please set CLOUDINARY_URL environment variable")
		return
	}

	// ใช้ CLOUDINARY_URL แบบรวม
	Cld, err = cloudinary.NewFromURL(cloudinaryURL)
	if err != nil {
		log.Printf("❌ Error initializing Cloudinary from URL: %v", err)
		log.Printf("💡 Make sure CLOUDINARY_URL format is: cloudinary://API_KEY:API_SECRET@CLOUD_NAME")
		return
	}

	log.Println("✅ Cloudinary initialized successfully from CLOUDINARY_URL")
}

// UploadImage อัพโหลดภาพไปยัง Cloudinary
func UploadImage(filePath string) (string, error) {
	if Cld == nil {
		return "", fmt.Errorf("cloudinary not initialized")
	}

	ctx := context.Background()

	// อัพโหลดภาพ
	uploadResult, err := Cld.Upload.Upload(ctx, filePath, uploader.UploadParams{
		Folder: "game-store", // โฟลเดอร์ใน Cloudinary
	})

	if err != nil {
		return "", err
	}

	fmt.Printf("✅ Image uploaded to Cloudinary: %s\n", uploadResult.SecureURL)
	return uploadResult.SecureURL, nil
}

// UploadImageFromBytes อัพโหลดภาพจาก byte data (สำหรับ multipart form)
func UploadImageFromBytes(fileBytes []byte, fileName string) (string, error) {
	if Cld == nil {
		return "", fmt.Errorf("cloudinary not initialized")
	}

	ctx := context.Background()

	uploadResult, err := Cld.Upload.Upload(ctx, fileBytes, uploader.UploadParams{
		Folder:   "game-store",
		PublicID: fileName,
	})

	if err != nil {
		return "", err
	}

	fmt.Printf("✅ Image uploaded to Cloudinary: %s\n", uploadResult.SecureURL)
	return uploadResult.SecureURL, nil
}

// DeleteImage ลบภาพจาก Cloudinary
func DeleteImage(imageURL string) error {
	if Cld == nil {
		return fmt.Errorf("cloudinary not initialized")
	}

	// แยก public_id จาก URL
	publicID := extractPublicID(imageURL)
	if publicID == "" {
		return fmt.Errorf("invalid image URL: %s", imageURL)
	}

	fmt.Printf("🗑️ Deleting image from Cloudinary: %s\n", publicID)

	ctx := context.Background()
	result, err := Cld.Upload.Destroy(ctx, uploader.DestroyParams{
		PublicID: publicID,
	})

	if err != nil {
		return fmt.Errorf("error deleting image: %v", err)
	}

	if result.Result == "ok" {
		fmt.Printf("✅ Successfully deleted image: %s\n", publicID)
	} else {
		fmt.Printf("⚠️ Cloudinary deletion result: %s\n", result.Result)
	}

	return nil
}

// extractPublicID แยก public_id จาก Cloudinary URL
func extractPublicID(url string) string {
	if !strings.Contains(url, "cloudinary.com") {
		return ""
	}

	// ตัวอย่าง URL: https://res.cloudinary.com/drafpbjnp/image/upload/v1234567/game-store/filename.jpg
	// เราต้องการ "game-store/filename"

	parts := strings.Split(url, "/upload/")
	if len(parts) < 2 {
		return ""
	}

	path := parts[1]

	// ลบ version ถ้ามี (v1234567/)
	if strings.HasPrefix(path, "v") {
		pathParts := strings.SplitN(path, "/", 2)
		if len(pathParts) > 1 {
			path = pathParts[1]
		}
	}

	// ลบ extension
	if idx := strings.LastIndex(path, "."); idx != -1 {
		path = path[:idx]
	}

	return path
}

// maskCloudinaryURL ป้องกันการแสดง credentials เต็มใน logs
func maskCloudinaryURL(url string) string {
	if url == "" {
		return "empty"
	}

	if len(url) <= 20 {
		return "***"
	}

	// cloudinary://API_KEY:API_SECRET@CLOUD_NAME
	// แสดงเฉพาะ cloud name และบางส่วนของ API key
	parts := strings.Split(url, "://")
	if len(parts) < 2 {
		return "invalid_format"
	}

	credentialPart := parts[1]
	atIndex := strings.Index(credentialPart, "@")
	if atIndex == -1 {
		return "invalid_format"
	}

	credentials := credentialPart[:atIndex]
	cloudName := credentialPart[atIndex+1:]

	// Mask credentials
	credParts := strings.Split(credentials, ":")
	if len(credParts) == 2 {
		apiKey := credParts[0]
		apiSecret := credParts[1]

		maskedKey := maskAPIKey(apiKey)
		maskedSecret := maskAPIKey(apiSecret)

		return fmt.Sprintf("cloudinary://%s:%s@%s", maskedKey, maskedSecret, cloudName)
	}

	return fmt.Sprintf("cloudinary://***@%s", cloudName)
}

// maskAPIKey ป้องกันการแสดง API key เต็มใน logs
func maskAPIKey(apiKey string) string {
	if len(apiKey) <= 8 {
		return "***"
	}
	return apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
}

// GetCloudinaryStatus ตรวจสอบสถานะ Cloudinary
func GetCloudinaryStatus() string {
	if Cld == nil {
		return "❌ Cloudinary not initialized"
	}
	return "✅ Cloudinary is ready"
}

// IsCloudinaryAvailable ตรวจสอบว่า Cloudinary พร้อมใช้งานไหม
func IsCloudinaryAvailable() bool {
	return Cld != nil
}
