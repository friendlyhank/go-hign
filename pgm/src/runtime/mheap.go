package runtime

// mSpanList heads a linked list of spans.
//链成链表的mSpanList
//go:notinheap
type mSpanList struct{
	first *mspan
	last *mspan
}

//内存的基本管理单元
//go:notinheap
type mspan struct {
}

// Initialize an empty doubly-linked list.
func (list *mSpanList) init() {
	list.first = nil
	list.last =  nil
}
